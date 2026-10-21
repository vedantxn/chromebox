package orchestrator

import (
	"database/sql"
	"errors"
	"fmt"
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
)

// maxUserIndex is the highest network index handed out; the one above it is
// the base VM's reserved identity.
const maxUserIndex = 16000

// Store is the orchestrator's database: what *should* exist. The agents say
// what actually runs, and the reconciler brings the two together.
type Store struct{ db *sql.DB }

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
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// InsertVM records a new VM on node, in state creating, and gives it the
// lowest network index nobody holds. Both happen in one transaction, so two
// concurrent creates can never be given the same address.
func (s *Store) InsertVM(id, node, base string, memMiB int) (api.VM, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return api.VM{}, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT net_index FROM vms ORDER BY net_index`)
	if err != nil {
		return api.VM{}, err
	}
	idx := 1
	for rows.Next() {
		var used int
		if err := rows.Scan(&used); err != nil {
			rows.Close()
			return api.VM{}, err
		}
		if used == idx {
			idx++
		} else if used > idx {
			break
		}
	}
	rows.Close()
	if idx > maxUserIndex {
		return api.VM{}, errors.New("no network addresses left")
	}
	t := now()
	if _, err := tx.Exec(`INSERT INTO vms (id, net_index, node, base, state, mem_mib, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, idx, node, base, StateCreating, memMiB, t, t); err != nil {
		return api.VM{}, err
	}
	if err := tx.Commit(); err != nil {
		return api.VM{}, err
	}
	return api.VM{ID: id, NetIndex: idx, Node: node, Base: base, State: StateCreating, MemMiB: memMiB, Created: t}, nil
}

func scanVM(sc interface{ Scan(...any) error }) (api.VM, error) {
	var v api.VM
	var updated string
	err := sc.Scan(&v.ID, &v.NetIndex, &v.Node, &v.Base, &v.State, &v.MemMiB, &v.Created, &updated)
	v.CDPPath = fmt.Sprintf("/v/%d", v.NetIndex)
	return v, err
}

const vmCols = `id, net_index, node, base, state, mem_mib, created_at, updated_at`

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

func (s *Store) DeleteVM(id string) error {
	_, err := s.db.Exec(`DELETE FROM vms WHERE id = ?`, id)
	return err
}

// UsedMem is the memory of the VMs assigned to each node. Counting from the
// database, not from what agents last reported, means a VM still being created
// already takes its space, so two creates cannot both claim the last slot.
func (s *Store) UsedMem() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT node, SUM(mem_mib) FROM vms WHERE state != ? GROUP BY node`, StateLost)
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
