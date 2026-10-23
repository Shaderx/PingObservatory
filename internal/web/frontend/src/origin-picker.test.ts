import { describe, expect, it } from 'vitest';
import { approximateOrigin } from './origin-picker';

describe('manual approximate map origin', () => {
  it('stores only coarse coordinates and accepts the equator and prime meridian', () => {
    expect(approximateOrigin(0.049, -0.049)).toEqual({ latitude: 0, longitude: 0 });
    expect(approximateOrigin(103.821, 1.341)).toEqual({ latitude: 1.3, longitude: 103.8 });
  });
  it('wraps a panned longitude and rejects unusable map coordinates', () => {
    expect(approximateOrigin(181.24, 45.06)).toEqual({ latitude: 45.1, longitude: -178.8 });
    for (const point of [[Infinity, 0], [0, NaN], [0, 91], [0, -91]]) expect(approximateOrigin(point[0], point[1])).toBeNull();
  });
});
