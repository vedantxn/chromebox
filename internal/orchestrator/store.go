package orchestrator

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: no cgo, so cross-compiling stays trivial

	"github.com/vedantxn/chromebox/internal/api"
)

// VM states.
const (
	StateCreating  = "creating"
	StateRunning   = "running"
	StateMigrating = "migrating"
	StateLost      = "lost"
	StateDeleting  = "deleting"
	StatePausing   = "pausing" // its snapshot is being taken and stored
	StatePaused    = "paused"  // stored in object storage; no node holds it
)

// maxUserIndex is the highest network index handed out; the one above it is
// the base VM's reserved identity.
const maxUserIndex = 16000

// Store is the orchestrator's database: what *should* exist. The agents say
// what actually runs, and the reconciler brings the two together.
type Store struct {
	db *sql.DB

	idxMu   sync.Mutex
	lastIdx int // the network index handed out last (see InsertVM)
}

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer at a time; avoids "database is locked" under concurrency
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS vms (
			id TEXT PRIMARY KEY,
			net_index INTEGER NOT NULL UNIQUE,
			node TEXT NOT NULL,
			base TEXT NOT NULL,
			state TEXT NOT NULL,
			mem_mib INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			name TEXT PRIMARY KEY,
			agent_url TEXT NOT NULL,
			cordoned INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, err
		}
	}
	// Added in M4; ALTER fails harmlessly if the column already exists.
	_, _ = db.Exec(`ALTER TABLE vms ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`)
	// Added with pause/resume: where a paused VM's snapshot is, and how much of
	// its lifetime was left when it was paused (the clock stops while paused).
	_, _ = db.Exec(`ALTER TABLE vms ADD COLUMN snap_prefix TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE vms ADD COLUMN remaining_s REAL NOT NULL DEFAULT 0`)
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// InsertVM records a new VM on node, in state creating, and gives it a network
// index nobody holds: the first free one after the index handed out last,
// wrapping round. Reusing the lowest free index instead would hand a new VM
// the address of one deleted a moment ago, whose process may still be exiting
// and holding its TAP device. All of it happens in one transaction, so two
// concurrent creates can never be given the same address.
func (s *Store) InsertVM(id, node, base string, memMiB int, expires time.Time) (api.VM, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return api.VM{}, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT net_index FROM vms`)
	if err != nil {
		return api.VM{}, err
	}
	used := map[int]bool{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return api.VM{}, err
		}
		used[n] = true
	}
	rows.Close()
	s.idxMu.Lock()
	defer s.idxMu.Unlock()
	idx := 0
	for i := 1; i <= maxUserIndex; i++ {
		if c := (s.lastIdx+i-1)%maxUserIndex + 1; !used[c] {
			idx = c
			break
		}
	}
	if idx == 0 {
		return api.VM{}, errors.New("no network addresses left")
	}
	t := now()
	exp := ""
	if !expires.IsZero() {
		exp = expires.UTC().Format(time.RFC3339Nano)
	}
	if _, err := tx.Exec(`INSERT INTO vms (id, net_index, node, base, state, mem_mib, created_at, updated_at, expires_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		id, idx, node, base, StateCreating, memMiB, t, t, exp); err != nil {
		return api.VM{}, err
	}
	if err := tx.Commit(); err != nil {
		return api.VM{}, err
	}
	s.lastIdx = idx
	return api.VM{ID: id, NetIndex: idx, Node: node, Base: base, State: StateCreating, MemMiB: memMiB, Created: t, Expires: exp}, nil
}

func scanVM(sc interface{ Scan(...any) error }) (api.VM, error) {
	var v api.VM
	var updated string
	err := sc.Scan(&v.ID, &v.NetIndex, &v.Node, &v.Base, &v.State, &v.MemMiB, &v.Created, &updated, &v.Expires)
	v.CDPPath = fmt.Sprintf("/v/%d", v.NetIndex)
	return v, err
}

const vmCols = `id, net_index, node, base, state, mem_mib, created_at, updated_at, expires_at`

func (s *Store) GetVM(id string) (api.VM, error) {
	return scanVM(s.db.QueryRow(`SELECT `+vmCols+` FROM vms WHERE id = ?`, id))
}

func (s *Store) ListVMs() ([]api.VM, error) {
	rows, err := s.db.Query(`SELECT ` + vmCols + ` FROM vms ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.VM
	for rows.Next() {
		v, err := scanVM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SetState(id, state string) error {
	_, err := s.db.Exec(`UPDATE vms SET state = ?, updated_at = ? WHERE id = ?`, state, now(), id)
	return err
}

func (s *Store) SetNode(id, node string) error {
	_, err := s.db.Exec(`UPDATE vms SET node = ?, updated_at = ? WHERE id = ?`, node, now(), id)
	return err
}

// SetPaused records that VM id now lives in object storage under prefix. Its
// node is empty, its base is the regular base its disk delta applies to, and
// remaining is how much lifetime it had left; it keeps its network index, so
// its address survives the pause.
func (s *Store) SetPaused(id, prefix, base string, remaining time.Duration) error {
	_, err := s.db.Exec(`UPDATE vms SET state = ?, node = '', base = ?, snap_prefix = ?, remaining_s = ?, updated_at = ? WHERE id = ?`,
		StatePaused, base, prefix, remaining.Seconds(), now(), id)
	return err
}

// SnapInfo is where paused VM id's snapshot is and how much lifetime it has left.
func (s *Store) SnapInfo(id string) (prefix string, remaining time.Duration, err error) {
	var r float64
	err = s.db.QueryRow(`SELECT snap_prefix, remaining_s FROM vms WHERE id = ?`, id).Scan(&prefix, &r)
	return prefix, time.Duration(r * float64(time.Second)), err
}

// SetResuming puts paused VM id on node, in state creating, so that its
// memory counts there while it is brought back.
func (s *Store) SetResuming(id, node string) error {
	_, err := s.db.Exec(`UPDATE vms SET state = ?, node = ?, updated_at = ? WHERE id = ?`, StateCreating, node, now(), id)
	return err
}

// SetResumed marks VM id running with a new expiry, and forgets its snapshot.
func (s *Store) SetResumed(id string, expires time.Time) error {
	_, err := s.db.Exec(`UPDATE vms SET state = ?, expires_at = ?, snap_prefix = '', remaining_s = 0, updated_at = ? WHERE id = ?`,
		StateRunning, expires.UTC().Format(time.RFC3339Nano), now(), id)
	return err
}

func (s *Store) DeleteVM(id string) error {
	_, err := s.db.Exec(`DELETE FROM vms WHERE id = ?`, id)
	return err
}

// UsedMem is the memory of the VMs assigned to each node. Counting from the
// database, not from what agents last reported, means a VM still being created
// already takes its space, so two creates cannot both claim the last slot.
func (s *Store) UsedMem() (map[string]int, error) {
	// A paused VM takes no node's memory: it is in object storage.
	rows, err := s.db.Query(`SELECT node, SUM(mem_mib) FROM vms WHERE state NOT IN (?, ?) GROUP BY node`, StateLost, StatePaused)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	used := map[string]int{}
	for rows.Next() {
		var n string
		var m int
		if err := rows.Scan(&n, &m); err != nil {
			return nil, err
		}
		used[n] = m
	}
	return used, rows.Err()
}

func (s *Store) UpsertNode(name, agentURL string) error {
	_, err := s.db.Exec(`INSERT INTO nodes (name, agent_url) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET agent_url = excluded.agent_url`, name, agentURL)
	return err
}

func (s *Store) SetCordoned(name string, v bool) error {
	n := 0
	if v {
		n = 1
	}
	res, err := s.db.Exec(`UPDATE nodes SET cordoned = ? WHERE name = ?`, n, name)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("unknown node %q", name)
	}
	return nil
}

type nodeRow struct {
	Name     string
	AgentURL string
	Cordoned bool
}

func (s *Store) ListNodes() ([]nodeRow, error) {
	rows, err := s.db.Query(`SELECT name, agent_url, cordoned FROM nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nodeRow
	for rows.Next() {
		var n nodeRow
		var c int
		if err := rows.Scan(&n.Name, &n.AgentURL, &c); err != nil {
			return nil, err
		}
		n.Cordoned = c == 1
		out = append(out, n)
	}
	return out, rows.Err()
}
