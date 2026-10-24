'use client';
import { useState } from 'react';
import { arena, facts } from './data';

// "A session can …": choosing a verb puts that action's own event log in front
// of the baton. The rows are the event kinds the API returns
// (GET /api/v1/sessions/:id/events), with measured times.

type Row = [time: string, kind: string, detail: string];
const items: { verb: string; fact: string; rows: Row[]; tone: 'olive' | 'stone' }[] = [
  {
    verb: 'move', tone: 'olive',
    fact: `Paused ${facts.movePaused} ms on average; ${facts.disconnects} disconnects in ${facts.moves} measured moves.`,
    rows: [
      ['14:02:11', 'created', `node-a · warm · ${arena.baton.create} ms`],
      ['14:06:40', 'migrated', `node-a → node-b · paused ${facts.movePaused} ms`],
      ['14:09:02', 'migrated', 'node-b → node-a · paused 208 ms'],
    ],
  },
  {
    verb: 'fork', tone: 'stone',
    fact: `The parent pauses ${facts.forkPaused} ms; three copies run in ${facts.fork3Ready} s, each with fresh randomness.`,
    rows: [
      ['14:03:05', 'forked', `into 3 · parent paused ${facts.forkPaused} ms`],
      ['14:03:07', 'created', 'vm-1c40e2 · node-a · copy'],
      ['14:03:07', 'created', 'vm-88b0a7 · node-b · copy'],
      ['14:03:08', 'created', 'vm-e4d913 · node-b · copy'],
    ],
  },
  {
    verb: 'sleep', tone: 'olive',
    fact: `Stored in ${facts.pause} s. While it sleeps no machine holds it, for up to 7 days.`,
    rows: [
      ['14:10:30', 'paused', 'stored in S3 · 1.9 s'],
      ['14:10:30', 'released', 'node-a has room again'],
    ],
  },
  {
    verb: 'wake', tone: 'stone',
    fact: `Back in ${facts.resume} s on whichever machine has room, with its pages and cookies.`,
    rows: [
      ['16:41:12', 'resumed', `node-c · ${facts.resume} s`],
      ['16:41:12', 'route', 'same address, new token'],
    ],
  },
  {
    verb: 'remember', tone: 'olive',
    fact: `Save a logged-in browser as a profile; later sessions start from it in ${facts.profileLater} s.`,
    rows: [
      ['15:20:44', 'profile', `“shop” saved · paused ${facts.profilePaused} ms`],
      ['09:02:16', 'started_from_profile', `“shop” · node-b · ${facts.profileLater} s`],
    ],
  },
];

export default function Abilities() {
  const [i, setI] = useState(0);
  const it = items[i];
  return (
    <div className="abilities">
      <div className={`abilities__stage tone-${it.tone}`}>
        <div className="abilities__baton" aria-hidden="true" />
        <figure className="abilities__card" data-ui key={it.verb}>
          <figcaption>vm-7f3a21 · events</figcaption>
          <table>
            <thead><tr><th>time</th><th>event</th><th>detail</th></tr></thead>
            <tbody>
              {it.rows.map((r, k) => <tr key={k}><td>{r[0]}</td><td>{r[1]}</td><td>{r[2]}</td></tr>)}
            </tbody>
          </table>
        </figure>
        <p className="abilities__fact">{it.fact}</p>
      </div>
      <div className="abilities__list">
        <h2 className="abilities__heading">A session can</h2>
        <ul role="tablist" aria-label="What a session can do">
          {items.map((x, k) => (
            <li key={x.verb}>
              <button type="button" role="tab" aria-selected={k === i} className={k === i ? 'is-on' : undefined}
                onClick={() => setI(k)} onMouseEnter={() => setI(k)}>
                {x.verb}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
