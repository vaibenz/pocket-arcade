import Phaser from 'phaser';
import { DOT, DotRound, dotLifetime, type Tap } from '../sim/dotrush';
import { MAX_FRAME_MS, PALETTE, type RunHooks } from './types';

/**
 * Dot Rush: tap each dot before it fades, 30 seconds per round. Dot
 * positions come from the session seed, so the server knows where every dot
 * was and replays the recorded taps against them.
 */
export class DotRushScene extends Phaser.Scene {
  private hooks!: RunHooks;
  private round!: DotRound;
  private taps: Tap[] = [];
  private t = 0; // ms into the round
  private unit = 1;
  private ox = 0;
  private oy = 0;
  private arena!: Phaser.GameObjects.Rectangle;
  private dot!: Phaser.GameObjects.Arc;
  private ring!: Phaser.GameObjects.Graphics;
  private shownIndex = -1;
  private ended = false;

  constructor() {
    super('dotrush');
  }

  init(hooks: RunHooks) {
    this.hooks = hooks;
    this.round = new DotRound(hooks.seed);
    this.taps = [];
    this.t = 0;
    this.shownIndex = -1;
    this.ended = false;
  }

  create() {
    this.cameras.main.setBackgroundColor(PALETTE.bg);
    this.arena = this.add.rectangle(0, 0, 1, 1, 0xffffff, 0.03).setOrigin(0, 0);
    this.ring = this.add.graphics();
    this.dot = this.add.circle(0, 0, 1, PALETTE.hues[0]);
    this.layout();
    const onResize = () => this.layout();
    this.scale.on('resize', onResize);
    // the scene is stopped and restarted for every run; drop the listener with it
    this.events.once('shutdown', () => this.scale.off('resize', onResize));
    this.input.on('pointerdown', (p: Phaser.Input.Pointer) => this.onTap(p));
    this.hooks.onHud({ score: 0, timeLeftMs: DOT.roundMs });
  }

  /** Letterboxes the 360 x 640 arena into whatever space the page gives us. */
  private layout() {
    const { width, height } = this.scale;
    this.unit = Math.min(width / DOT.arenaW, height / DOT.arenaH);
    this.ox = (width - DOT.arenaW * this.unit) / 2;
    this.oy = (height - DOT.arenaH * this.unit) / 2;
    this.arena.setPosition(this.ox, this.oy).setSize(DOT.arenaW * this.unit, DOT.arenaH * this.unit);
    this.shownIndex = -1; // redraw at the new scale
  }

  private toScreen(x: number, y: number) {
    return { x: this.ox + x * this.unit, y: this.oy + y * this.unit };
  }

  update(_time: number, delta: number) {
    if (this.ended) return;
    this.t += Math.min(delta, MAX_FRAME_MS);
    if (this.t >= DOT.roundMs) return this.finish();
    this.round.advance(Math.floor(this.t));
    if (this.round.index !== this.shownIndex) this.showDot();

    // a ring that closes as the dot's time runs out
    const left = (this.round.deadline - this.t) / dotLifetime(this.round.index);
    const c = this.toScreen(this.round.dot.x, this.round.dot.y);
    this.ring.clear();
    this.ring.lineStyle(3 * this.unit, PALETTE.ink, 0.6);
    this.ring.beginPath();
    this.ring.arc(c.x, c.y, (DOT.radius + 8) * this.unit, -Math.PI / 2, -Math.PI / 2 + Math.max(left, 0) * Math.PI * 2);
    this.ring.strokePath();
    this.hooks.onHud({ score: this.round.score, timeLeftMs: DOT.roundMs - this.t });
  }

  private showDot() {
    this.shownIndex = this.round.index;
    const c = this.toScreen(this.round.dot.x, this.round.dot.y);
    this.dot.setPosition(c.x, c.y).setRadius(DOT.radius * this.unit);
    this.dot.setFillStyle(PALETTE.hues[this.round.index % PALETTE.hues.length]);
    this.dot.setScale(0.6);
    this.tweens.add({ targets: this.dot, scale: 1, duration: 120, ease: 'Back.easeOut' });
  }

  private onTap(p: Phaser.Input.Pointer) {
    if (this.ended) return;
    const tap: Tap = {
      t: Math.floor(this.t),
      x: Math.round((p.x - this.ox) / this.unit),
      y: Math.round((p.y - this.oy) / this.unit),
    };
    const before = { ...this.round.dot };
    const result = this.round.tap(tap);
    if (result === 'ignored') return;
    this.taps.push(tap);
    if (result === 'hit') {
      this.hooks.onHaptic('light');
      this.burst(before.x, before.y);
    } else {
      this.ripple(p.x, p.y);
    }
  }

  private burst(x: number, y: number) {
    const c = this.toScreen(x, y);
    const pop = this.add.circle(c.x, c.y, DOT.radius * this.unit, PALETTE.ink, 0.5);
    this.tweens.add({ targets: pop, scale: 1.8, alpha: 0, duration: 220, onComplete: () => pop.destroy() });
  }

  private ripple(x: number, y: number) {
    const r = this.add.circle(x, y, 10 * this.unit).setStrokeStyle(2, PALETTE.miss);
    this.tweens.add({ targets: r, scale: 2.5, alpha: 0, duration: 260, onComplete: () => r.destroy() });
  }

  private finish() {
    this.ended = true;
    this.dot.setVisible(false);
    this.ring.clear();
    this.hooks.onHud({ score: this.round.score, timeLeftMs: 0 });
    this.hooks.onHaptic('success');
    this.time.delayedCall(400, () => this.hooks.onEnd({ game: 'dotrush', score: this.round.score, taps: this.taps }));
  }
}
