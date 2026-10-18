/** Demand pacing uses rendered frames and monotonic durations, never active-count caps. */
export const DEMAND_PACING = Object.freeze({
  horizonMs: 5000, maximumGapMs: 1500, initialFrameMs: 1000 / 60,
  healthyFrameRatio: 1.5, frameSmoothing: 0.1,
  constructionBudgetRatio: 0.5, healthyRecovery: 0.95,
});
export type DemandPlan = {
  runnableLocations: number; desiredGapMs: number; effectiveGapMs: number;
  frameIntervalMs: number; healthyFrameMs: number; pressureMultiplier: number;
  constructionMs: number; waitMs: number; ready: boolean;
};

export class DemandPacer {
  private previousFrame?: number;
  private healthyFrame?: number;
  private interval = DEMAND_PACING.initialFrameMs;
  private pressure = 1;
  private construction = 0;
  private lastLaunch?: number;
  private launchedFrame?: number;

  /** Called only while visible, admitted work exists. Pauses discard elapsed gaps. */
  observeFrame(frame: number) {
    const previous = this.previousFrame;
    if (previous !== undefined && frame <= previous) return;
    this.previousFrame = frame;
    if (previous === undefined) return;
    const interval = frame - previous;
    this.interval = interval;
    if (this.healthyFrame === undefined) {
      this.healthyFrame = interval;
    } else if (interval > this.healthyFrame * DEMAND_PACING.healthyFrameRatio) {
      this.pressure = Math.max(this.pressure, interval / this.healthyFrame);
      return;
    } else {
      this.healthyFrame += (interval - this.healthyFrame) * DEMAND_PACING.frameSmoothing;
    }
    this.pressure = Math.max(1, this.pressure * DEMAND_PACING.healthyRecovery);
  }

  plan(now: number, frame: number, runnableLocations: number, earliestExpiry: number): DemandPlan {
    const healthy = this.healthyFrame ?? DEMAND_PACING.initialFrameMs;
    const deadline = Math.max(0, earliestExpiry - now);
    const desired = runnableLocations === 0 ? 0 : Math.min(
      DEMAND_PACING.maximumGapMs,
      DEMAND_PACING.horizonMs / runnableLocations,
      Math.max(0, deadline - healthy) / (runnableLocations + 1),
    );
    // Rendered frames are the physical start fence, not a fixed concurrency limit.
    const effective = runnableLocations === 0 ? 0 : Math.max(healthy, desired) * this.pressure;
    const wait = runnableLocations === 0 || this.lastLaunch === undefined ? 0 : Math.max(0, this.lastLaunch + effective - now);
    return {
      runnableLocations, desiredGapMs: desired, effectiveGapMs: effective,
      frameIntervalMs: this.interval, healthyFrameMs: healthy,
      pressureMultiplier: this.pressure, constructionMs: this.construction, waitMs: wait,
      ready: runnableLocations > 0 && this.launchedFrame !== frame && wait === 0,
    };
  }

  launched(frame: number, start: number, constructionMs: number) {
    this.launchedFrame = frame;
    this.lastLaunch = start; // One start; no accumulated catch-up credits.
    this.construction = Math.max(0, constructionMs);
    const budget = (this.healthyFrame ?? DEMAND_PACING.initialFrameMs) * DEMAND_PACING.constructionBudgetRatio;
    if (this.construction > budget) this.pressure = Math.max(this.pressure, this.construction / budget);
  }

  pauseFrames() { this.previousFrame = undefined; }
  idle() { this.pauseFrames(); this.lastLaunch = undefined; }
}
