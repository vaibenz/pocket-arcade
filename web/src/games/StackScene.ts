import Phaser from 'phaser';
import { STACK, stackDrop, stackPosition, stackStart, type StackState } from '../sim/stack';
import { MAX_FRAME_MS, PALETTE, type RunHooks } from './types';

const BLOCK_H = 26; // in board units, like the 400 unit board width

/**
 * Stack: tap to drop the sliding block onto the tower. The scene only draws
 * and records timing; every rule lives in sim/stack.ts so the server can
 * replay the run exactly.
 */
export class StackScene extends Phaser.Scene {
  private hooks!: RunHooks;
  private state: StackState = stackStart();
  private taps: number[] = [];
  private elapsed = 0; // ms since the current block spawned
  private unit = 1; // pixels per board unit
  private tower!: Phaser.GameObjects.Container;
  private moving!: Phaser.GameObjects.Rectangle;
  private ended = false;

  constructor() {
    super('stack');
  }

  init(hooks: RunHooks) {
    this.hooks = hooks;
    this.state = stackStart();
    this.taps = [];
    this.elapsed = 0;
    this.ended = false;
  }

  create() {
    this.cameras.main.setBackgroundColor(PALETTE.bg);
    this.tower = this.add.container(0, 0);
    this.layout();
    this.addBlock(-1, STACK.startX, STACK.startWidth);
    this.moving = this.add.rectangle(0, 0, 1, 1, this.colour(0)).setOrigin(0, 0);
    this.tower.add(this.moving);
    this.placeMoving();

    this.input.on('pointerdown', () => this.drop());
    this.input.keyboard?.on('keydown-SPACE', () => this.drop());
    const onResize = () => this.layout();
    this.scale.on('resize', onResize);
    // the scene is stopped and restarted for every run; drop the listener with it
    this.events.once('shutdown', () => this.scale.off('resize', onResize));
    this.hooks.onHud({ score: 0, note: 'Tap to drop' });
  }

  private layout() {
    const { width, height } = this.scale;
    // fit the board to the width, but keep blocks a sensible height on wide screens
    this.unit = Math.min(width / STACK.boardWidth, height / 520);
    this.tower.x = (width - STACK.boardWidth * this.unit) / 2;
    this.tower.setScale(this.unit);
    this.scrollTo(false);
  }

  // y in board units for block index i (-1 is the base); the tower grows upward
  private blockY(i: number) {
    return -(i + 2) * BLOCK_H;
  }

  private scrollTo(animate: boolean) {
    const { height } = this.scale;
    // start with the base low on screen, then keep the sliding block about 40% down
    const follow = height * 0.4 - this.blockY(this.state.placed) * this.unit;
    const y = Math.max(follow, height * 0.75 - this.blockY(-1) * this.unit);
    if (animate) this.tweens.add({ targets: this.tower, y, duration: 180, ease: 'Sine.easeOut' });
    else this.tower.y = y;
  }

  private colour(i: number) {
    return PALETTE.hues[Math.floor((i + 1) / 6) % PALETTE.hues.length];
  }

  private addBlock(i: number, x: number, w: number) {
    const r = this.add.rectangle(x, this.blockY(i), w, BLOCK_H - 2, this.colour(i)).setOrigin(0, 0);
    this.tower.add(r);
    return r;
  }

  private placeMoving() {
    const i = this.state.placed;
    this.moving.setPosition(stackPosition(i, this.state.w, this.elapsed), this.blockY(i));
    this.moving.setSize(this.state.w, BLOCK_H - 2);
    this.moving.setFillStyle(this.colour(i));
  }

  update(_time: number, delta: number) {
    if (this.ended) return;
    this.elapsed += Math.min(delta, MAX_FRAME_MS);
    if (this.elapsed >= STACK.maxTapMs) this.drop(); // an idle player still gets a result
    this.placeMoving();
  }

  private drop() {
    if (this.ended || this.elapsed < STACK.minTapMs) return;
    const dt = Math.min(Math.round(this.elapsed), STACK.maxTapMs);
    const i = this.state.placed;
    const [next, out] = stackDrop(this.state, dt);
    this.taps.push(dt);
    this.state = next;
    this.elapsed = 0;

    if (out.kind === 'miss') {
      this.ended = true;
      this.moving.setFillStyle(PALETTE.miss);
      this.tweens.add({ targets: this.moving, y: this.moving.y + 600, angle: 25, duration: 700, ease: 'Quad.easeIn' });
      this.hooks.onHaptic('error');
      this.time.delayedCall(750, () =>
        this.hooks.onEnd({ game: 'stack', score: this.state.score, taps: this.taps }),
      );
      return;
    }

    const placed = this.addBlock(i, out.x, out.w);
    if (out.kind === 'perfect') {
      this.hooks.onHaptic('medium');
      this.tweens.add({ targets: placed, alpha: { from: 0.4, to: 1 }, duration: 220 });
      this.hooks.onHud({ score: this.state.score, note: 'Perfect +2' });
    } else {
      this.hooks.onHaptic('light');
      const cut = this.add.rectangle(out.cutX, this.blockY(i), out.cutW, BLOCK_H - 2, this.colour(i)).setOrigin(0, 0);
      this.tower.add(cut);
      this.tweens.add({
        targets: cut,
        y: cut.y + 500,
        alpha: 0,
        angle: out.cutX < out.x ? -20 : 20,
        duration: 650,
        ease: 'Quad.easeIn',
        onComplete: () => cut.destroy(),
      });
      this.hooks.onHud({ score: this.state.score });
    }
    this.tower.bringToTop(this.moving);
    this.scrollTo(true);
    this.placeMoving();
  }
}
