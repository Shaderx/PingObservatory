import { describe, expect, it } from 'vitest';
import { DEMAND_PACING, DemandPacer } from './demand-pacing';

describe('frame-driven demand pacing', () => {
  it('starts idle work promptly and launches at most once per rendered frame', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0);
    expect(pacer.plan(0, 0, 1, 15000).ready).toBe(true);
    pacer.launched(0, 0, 0);
    expect(pacer.plan(10000, 0, 100, 15000).ready).toBe(false);
    pacer.pauseFrames(); pacer.observeFrame(10000);
    expect(pacer.plan(10000, 10000, 100, 15000).ready).toBe(true);
    pacer.launched(10000, 10000, 0);
    expect(pacer.plan(10001, 10000, 100, 15000).ready).toBe(false);
    expect(pacer.plan(10001, 10001, 100, 15000).ready).toBe(false);
  });

  it('uses the deadline and runnable location heads, with no fixed start floor or overlap cap', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(10);
    expect(pacer.plan(10, 10, 1, 15010).desiredGapMs).toBe(1500);
    expect(pacer.plan(10, 10, 10, 15010).desiredGapMs).toBe(500);
    expect(pacer.plan(10, 10, 100, 15010).desiredGapMs).toBe(50);
    expect(pacer.plan(10, 10, 1000, 15010).desiredGapMs).toBe(5);
    expect(pacer.plan(10, 10, 1000, 15010).effectiveGapMs).toBe(10);
    expect(pacer.plan(10, 10, 10, 120).desiredGapMs).toBeCloseTo(100 / 11);
    expect(pacer.plan(10, 10, 10, 10).desiredGapMs).toBe(0);
    expect(pacer.plan(10, 10, 0, 10)).toMatchObject({ ready: false, desiredGapMs: 0, effectiveGapMs: 0 });
  });

  it('fences dense launches with actual frames without accumulating catch-up credit', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(20);
    const launches: number[] = [];
    for (let frame = 20; frame <= 200; frame += 20) {
      pacer.observeFrame(frame);
      if (pacer.plan(frame, frame, 1000, 15000).ready) { launches.push(frame); pacer.launched(frame, frame, 0); }
      expect(pacer.plan(frame, frame, 1000, 15000).ready).toBe(false);
    }
    expect(launches).toHaveLength(10); // More than five active 1.8s sequences is permitted.
    pacer.pauseFrames(); pacer.observeFrame(10000);
    expect(pacer.plan(10000, 10000, 1000, 15000).ready).toBe(true);
    pacer.launched(10000, 10000, 0);
    expect(pacer.plan(10000, 10000, 1000, 15000).ready).toBe(false);
    expect(pacer.plan(10001, 10001, 1000, 15000)).toMatchObject({ ready: false, waitMs: 19 });
  });

  it('learns a healthy frame interval, including a native slower display', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(100); pacer.observeFrame(133);
    expect(pacer.plan(133, 133, 100, 15000)).toMatchObject({ healthyFrameMs: 33, frameIntervalMs: 33, pressureMultiplier: 1 });
    pacer.observeFrame(166);
    expect(pacer.plan(166, 166, 100, 15000).pressureMultiplier).toBe(1);
  });

  it('slows on measured long frames and recovers gradually only with healthy frames', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(20); pacer.launched(20, 20, 0);
    pacer.observeFrame(100); // 80ms against measured 20ms.
    const pressured = pacer.plan(100, 100, 100, 15000);
    expect(pressured.pressureMultiplier).toBe(4);
    expect(pressured.effectiveGapMs).toBe(200);
    expect(pressured.ready).toBe(false);
    expect(pressured.waitMs).toBe(120);
    pacer.observeFrame(120);
    expect(pacer.plan(120, 120, 100, 15000).pressureMultiplier).toBeCloseTo(4 * DEMAND_PACING.healthyRecovery);
    for (let frame = 140; frame <= 1200; frame += 20) pacer.observeFrame(frame);
    expect(pacer.plan(1200, 1200, 100, 15000).pressureMultiplier).toBe(1);
  });

  it('uses measured construction cost as delay pressure, without a numerical pressure ceiling', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(20);
    pacer.launched(20, 20, 1000); // 100 times the 10ms frame construction budget.
    const plan = pacer.plan(40, 40, 100, 15000);
    expect(plan).toMatchObject({ constructionMs: 1000, pressureMultiplier: 100, effectiveGapMs: 5000, ready: false });
    pacer.observeFrame(40);
    expect(pacer.plan(40, 40, 100, 15000).pressureMultiplier).toBe(95);
  });

  it('does not count hidden, paused or idle gaps as frame pressure or healthy recovery', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(20); pacer.launched(20, 20, 50);
    const pressure = pacer.plan(20, 20, 100, 15000).pressureMultiplier;
    pacer.pauseFrames(); pacer.observeFrame(3600000);
    expect(pacer.plan(3600000, 3600000, 100, 3615000).pressureMultiplier).toBe(pressure);
    expect(pacer.plan(3600000, 3600000, 100, 3615000).frameIntervalMs).toBe(20);
    pacer.idle(); pacer.observeFrame(7200000);
    expect(pacer.plan(7200000, 7200000, 1, 7215000).ready).toBe(true);
    expect(pacer.plan(7200000, 7200000, 1, 7215000).pressureMultiplier).toBe(pressure);
  });

  it('ignores repeated or retired frame timestamps without corrupting the interval', () => {
    const pacer = new DemandPacer();
    pacer.observeFrame(0); pacer.observeFrame(20);
    pacer.observeFrame(20); pacer.observeFrame(10); pacer.observeFrame(40);
    expect(pacer.plan(40, 40, 10, 15000)).toMatchObject({ frameIntervalMs: 20, healthyFrameMs: 20, pressureMultiplier: 1 });
  });
});
