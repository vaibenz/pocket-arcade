import type Phaser from 'phaser';
import { ApiClient, ApiError, type Session } from './api/client';
import { Bridge, BRIDGE_VERSION, type GameId } from './bridge/bridge';
import { bindSafeArea } from './bridge/safeArea';
import { PALETTE, type RunEnd, type RunHooks } from './games/types';
import { loadPlayer, renamePlayer, type Player } from './player';
import './styles.css';

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;
const TITLES: Record<GameId, string> = { stack: 'Stack', dotrush: 'Dot Rush' };

const bridge = new Bridge();
const api = new ApiClient(import.meta.env.VITE_API_BASE ?? '');
let player: Player = loadPlayer();
bindSafeArea(bridge);

type Screen = 'home' | 'play' | 'result';
let screen: Screen = 'home';
function show(next: Screen) {
  // Phaser polls its parent's size every frame. Sleep the loop before the stage
  // is hidden, or it shrinks the canvas to 0 x 0 and WebGL loses its framebuffer.
  // A sleeping loop also stops drawing, which saves battery between runs.
  if (next !== 'play' && game && game.loop.running) game.loop.sleep();
  for (const s of ['home', 'play', 'result'] as const) $(s).hidden = s !== next;
  screen = next;
}

// ---- local bests, shown on the home cards even when offline ----
function localBest(game: GameId): number {
  try {
    return Number(localStorage.getItem(`pocket-arcade.best.${game}`)) || 0;
  } catch {
    return 0;
  }
}
function saveLocalBest(game: GameId, score: number) {
  try {
    if (score > localBest(game)) localStorage.setItem(`pocket-arcade.best.${game}`, String(score));
  } catch {
    /* ignore */
  }
}
function renderHome() {
  $<HTMLInputElement>('name').value = player.name;
  $<HTMLInputElement>('name').disabled = player.fromHost;
  for (const el of document.querySelectorAll<HTMLElement>('[data-best]')) {
    const b = localBest(el.dataset.best as GameId);
    el.textContent = b ? `Your best: ${b}` : 'Not played yet';
  }
}

$<HTMLInputElement>('name').addEventListener('change', (e) => {
  player = renamePlayer(player, (e.target as HTMLInputElement).value);
  renderHome();
});

bridge.on('player', (e) => {
  player = { id: e.id, name: e.name || player.name, fromHost: true };
  renderHome();
});

// ---- the Phaser game, created once and reused for every run ----
// Phaser is most of the bundle, so the home screen loads without it and the
// engine is fetched while the player is still choosing a game.
let game: Phaser.Game | null = null;
const importEngine = () => Promise.all([import('phaser'), import('./games/StackScene'), import('./games/DotRushScene')]);
let modules: ReturnType<typeof importEngine> | null = null;
function loadEngine() {
  modules ??= importEngine();
  modules.catch(() => (modules = null)); // allow a retry after a failed download
  return modules;
}

/** Creates the game on first use. Call it only once #stage is visible, so the canvas is sized to real space. */
async function phaser(): Promise<Phaser.Game> {
  const [mod, { StackScene }, { DotRushScene }] = await loadEngine();
  // phaser is CommonJS; bundlers expose it as the default export
  const P = ((mod as unknown as { default?: typeof Phaser }).default ?? mod) as typeof Phaser;
  if (game) return game;
  game = createGame(P);
  game.scene.add('stack', StackScene, false);
  game.scene.add('dotrush', DotRushScene, false);
  // the stage changes size with rotation, keyboards, browser bars and split screen
  const g = game;
  // skip while hidden: a 0 x 0 canvas leaves WebGL with an incomplete framebuffer
  new ResizeObserver(() => $('stage').clientWidth > 0 && g.scale.refresh()).observe($('stage'));
  return game;
}

function createGame(P: typeof Phaser): Phaser.Game {
  return new P.Game({
    type: P.AUTO,
    parent: 'stage',
    backgroundColor: PALETTE.bg,
    scale: { mode: P.Scale.RESIZE, width: '100%', height: '100%' },
    input: { activePointers: 2 },
    fps: { target: 60, smoothStep: true },
    render: { antialias: true, powerPreference: 'low-power' }, // easier on phone batteries
    banner: false,
    scene: [],
  });
}

// ---- a single run ----
interface Run {
  game: GameId;
  session: Session | null; // null when offline: play anyway, just do not submit
  paused: boolean;
}
let run: Run | null = null;

function overlay(text: string, actions: { label: string; primary?: boolean; onClick(): void }[] = []) {
  $('overlay').hidden = false;
  $('overlay-text').textContent = text;
  const box = $('overlay-actions');
  box.replaceChildren(
    ...actions.map((a) => {
      const b = document.createElement('button');
      b.textContent = a.label;
      if (a.primary) b.className = 'primary';
      b.addEventListener('click', a.onClick);
      return b;
    }),
  );
  (box.querySelector('button') as HTMLButtonElement | null)?.focus();
}
const hideOverlay = () => ($('overlay').hidden = true);

let noteTimer = 0;
function hooksFor(seed: number): RunHooks {
  return {
    seed,
    onHud({ score, timeLeftMs, note }) {
      $('score').textContent = String(score);
      $('timer').textContent = timeLeftMs === undefined ? '' : `${Math.ceil(timeLeftMs / 1000)}s`;
      if (note) {
        const n = $('note');
        n.textContent = note;
        n.style.opacity = '1';
        clearTimeout(noteTimer);
        noteTimer = window.setTimeout(() => (n.style.opacity = '0'), 900);
      }
    },
    onHaptic: (style) => bridge.send({ type: 'haptic', style }),
    onEnd: (end) => void finish(end),
  };
}

async function start(id: GameId) {
  show('play');
  $('score').textContent = '0';
  $('timer').textContent = '';
  run = { game: id, session: null, paused: false };
  const thisRun = run;
  // fetch the session and the engine during the countdown, so neither wait shows
  const sessionP = api.startSession(id, player.id, player.name).catch(() => null);
  const gameP = phaser(); // #stage is visible now, so the canvas gets its real size
  gameP.catch(() => {});
  for (const n of ['3', '2', '1']) {
    overlay(n);
    await new Promise((r) => setTimeout(r, 550));
    if (run !== thisRun) return; // the player left during the countdown
  }
  let g: Phaser.Game;
  try {
    g = await gameP;
  } catch {
    overlay('Could not load', [{ label: 'Back', onClick: quit }]);
    return;
  }
  if (run !== thisRun) return;
  if (!g.loop.running) g.loop.wake();
  g.scale.refresh();
  for (const k of ['stack', 'dotrush']) if (g.scene.isActive(k) || g.scene.isPaused(k)) g.scene.stop(k);
  thisRun.session = await sessionP;
  hideOverlay();
  const seed = thisRun.session?.seed ?? crypto.getRandomValues(new Uint32Array(1))[0];
  g.scene.start(id, hooksFor(seed));
  bridge.send({ type: 'gameStarted', game: id });
  if (!thisRun.session) hooksFor(seed).onHud({ score: 0, note: 'Offline: this run will not be saved' });
}

function pause() {
  if (!run || run.paused || screen !== 'play' || !game) return;
  if (!game.scene.isActive(run.game)) return;
  run.paused = true;
  game.scene.pause(run.game);
  overlay('Paused', [
    { label: 'Resume', primary: true, onClick: resume },
    { label: 'Quit', onClick: quit },
  ]);
}

function resume() {
  if (!run || !run.paused || !game) return;
  run.paused = false;
  hideOverlay();
  game.scene.resume(run.game);
}

function quit() {
  if (run && game) game.scene.stop(run.game);
  run = null;
  hideOverlay();
  show('home');
  renderHome();
}

$('pause').addEventListener('click', pause);
bridge.on('pause', pause); // app backgrounded, call came in, tab hidden
bridge.on('back', () => {
  if (screen === 'play') pause();
  else if (screen === 'result') quit();
  else bridge.send({ type: 'close' });
});

// ---- results ----
async function finish(end: RunEnd) {
  const r = run;
  if (!r || !game) return;
  game.scene.stop(end.game);
  bridge.send({ type: 'gameEnded', game: end.game, score: end.score });
  saveLocalBest(end.game, end.score);
  show('result');
  $('result-title').textContent = TITLES[end.game];
  $('result-score').textContent = String(end.score);
  $('board').replaceChildren();
  const detail = $('result-detail');

  if (!r.session) {
    detail.textContent = 'Played offline, so this score stays on this device.';
    return;
  }
  detail.textContent = 'Checking your run…';
  try {
    const res = await api.submit(r.session.token, end.score, end.taps);
    const parts = [res.personalBest ? 'New personal best!' : `Your best is ${res.best}.`];
    if (res.rank) parts.push(`You are #${res.rank}.`);
    detail.textContent = parts.join(' ');
    bridge.send({ type: 'scoreSubmitted', game: end.game, score: res.score, best: res.best, rank: res.rank });
    if (res.personalBest) bridge.send({ type: 'haptic', style: 'success' });
  } catch (e) {
    detail.textContent =
      e instanceof ApiError && e.code === 'expired'
        ? 'This session expired before the score was sent.'
        : e instanceof ApiError && e.status === 0
          ? 'Could not reach the server, so this score was not saved.'
          : 'The server could not verify this run, so it was not saved.';
  }
  await renderBoard(end.game);
}

async function renderBoard(id: GameId) {
  try {
    const lb = await api.leaderboard(id, player.id, 10);
    const rows = lb.entries.map((e) => row(e.rank, e.name, e.score, e.playerId === player.id));
    if (lb.you && !lb.entries.some((e) => e.playerId === player.id)) rows.push(row(lb.you.rank, player.name, localBest(id), true));
    $('board').replaceChildren(...rows);
  } catch {
    /* the result still stands without a leaderboard */
  }
}

function row(rank: number, name: string, score: number, me: boolean) {
  const li = document.createElement('li');
  if (me) li.className = 'me';
  for (const [cls, text] of [['rank', `#${rank}`], ['who', name], ['pts', String(score)]]) {
    const s = document.createElement('span');
    s.className = cls;
    s.textContent = text; // names come from other players: text only, never HTML
    li.append(s);
  }
  return li;
}

$('again').addEventListener('click', () => run && void start(run.game));
$('home-btn').addEventListener('click', quit);

const share = $('share');
share.hidden = bridge.transport === 'browser' && !navigator.share;
share.addEventListener('click', () => {
  if (!run) return;
  const text = `I scored ${$('result-score').textContent} in ${TITLES[run.game]} on Pocket Arcade`;
  if (bridge.transport !== 'browser') bridge.send({ type: 'share', text });
  else navigator.share?.({ text }).catch(() => {});
});

for (const card of document.querySelectorAll<HTMLButtonElement>('.card')) {
  card.addEventListener('click', () => void start(card.dataset.game as GameId));
}

renderHome();
show('home');
// warm the engine once the home screen is up
const idle = (window as Window & { requestIdleCallback?: (cb: () => void) => void }).requestIdleCallback ?? ((cb: () => void) => setTimeout(cb, 1200));
idle(() => void loadEngine().catch(() => {}));
bridge.send({ type: 'ready', version: BRIDGE_VERSION, transport: bridge.transport });
