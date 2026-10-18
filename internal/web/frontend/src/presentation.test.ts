import { describe, expect, it } from 'vitest';
import { emptyObservation, presentTargets } from './model';
import { mapLoss } from './map-loss';
import { mapLabelState } from './map-label-state';
import { compactProvider } from './compact-display';
import { distanceColor, distanceTiming } from './distance-color';
import { displayLocation } from './display-location';
import { needsReadingList } from './callout-capacity';
import { acceptLocalSnapshot, batchArrivalKey, createBatchArrivalTracker, markDeliveryUnavailable } from './batch-arrivals';
import type { Observation, Target } from './types';

const now = Date.parse('2026-10-18T12:00:00Z'), at = new Date(now).toISOString();
const observation = (changes: Partial<Observation> = {}): Observation => ({ ...emptyObservation('icmp','ipv4'), status:'ok', observed_at:at, rolling_as_of:at, baseline_as_of:at, baseline_sample_count:5, median_ms:30,baseline_ms:20,sent:3,received:3,loss_pct:0,...changes });
const target = (changes: Partial<Target> = {}): Target => ({ id:'fixture',revision:1,label:'Original full target',host:'fixture.example',provider:'Amazon Web Services',category:'fixture',region:'Singapore',country_code:'SG',world_area:'Asia',role:'test',enabled:true,measurement:{method:'icmp',families:['ipv4'],interval_seconds:60,packets_per_cycle:3,packet_spacing_ms:100,timeout_ms:1000,payload_bytes:32},geography:{coordinates:[103.8,1.3]},observations:[observation()],...changes });

describe('standalone presentation semantics', () => {
  it('uses non-RTT failure rates, never substitutes unknown with zero, and retains tiny positive loss', () => {
    expect(mapLoss(observation({method:'tcp_connect',loss_pct:null,failure_pct:25}),now)).toMatchObject({visible:true,scale:.25,unknown:false});
    const tiny=mapLoss(observation({loss_pct:.001}),now);
    expect(tiny.scale).toBe(.00001); expect(tiny.label).toContain('<0.1%');
    for(const value of [null,NaN,Infinity,-1,101]) expect(mapLoss(observation({loss_pct:value}),now)).toMatchObject({unknown:true,visible:false});
    expect(mapLoss(observation({loss_pct:0}),now).visible).toBe(false);
  });
  it('keeps baseline policy, measurement failures and local inability distinct', () => {
    expect(mapLabelState(target(),observation(),now)).toMatchObject({cue:'performance',elevatedTiming:true});
    for(const changes of [{baseline_sample_count:4},{baseline_as_of:new Date(now-181000).toISOString()},{median_ms:29.9},{baseline_ms:0}]) expect(mapLabelState(target(),observation(changes),now).elevatedTiming).toBe(false);
    expect(mapLabelState(target(),observation({status:'timeout',received:0,median_ms:null,loss_pct:100}),now).cue).toBe('failure');
    expect(mapLabelState(target(),observation({status:'timeout',sent:0,received:0,unable_to_measure:1,median_ms:null,loss_pct:0}),now).cue).toBe('information');
    expect(mapLabelState(target(),observation({status:'permission_denied',sent:0,received:0,unable_to_measure:1,median_ms:null,loss_pct:null}),now).cue).toBe('information');
    expect(mapLabelState(target(),observation({status:'dns_error',sent:0,median_ms:null}),now).description).toContain('DNS error');
    expect(compactProvider(target().provider)).toBe('AWS');
    expect(mapLabelState(target(),observation(),now).description).toContain('Amazon Web Services');
  });
  it('uses configured 0/0 origin and real target coordinates; heuristic never changes health', () => {
    const t=target({geography:{coordinates:[0,0]}}),o=observation({median_ms:10});
    expect(distanceTiming(t,o,[0,0],now)).toMatchObject({distanceKm:0,referenceMS:10,ratio:1});
    const neutral=distanceColor(t,o,undefined,now,false);
    expect(distanceColor(t,{...o,method:'https'},[0,0],now,false)).toBe(neutral);
    expect(distanceColor(t,{...o,observed_at:new Date(now-181000).toISOString()},[0,0],now,false)).toBe(neutral);
    expect(distanceColor(t,{...o,median_ms:NaN},[0,0],now,false)).toBe(neutral);
    expect(distanceColor(t,o,[Infinity,0],now,false)).toBe(neutral);
    expect(o.status).toBe('ok');
    const before=JSON.stringify(t);displayLocation(t);distanceColor(t,o,[150,-30],now,false);expect(JSON.stringify(t)).toBe(before);
  });
  it('groups display regions without changing provider, coordinates or identities', () => {
    const a=target(),b=target({id:'other',region:'SIN',geography:{coordinates:[103.9,1.4]}});
    expect(displayLocation(a)?.id).toBe(displayLocation(b)?.id);expect(a.geography.coordinates).toEqual([103.8,1.3]);
    expect(displayLocation(target({geography:{coordinates:null}}))).toBeUndefined();
  });
  it('requires a reading list when measured labels overlap, exceed bounds or cover controls', () => {
    const rect={left:80,top:20,width:100,height:45};
    expect(needsReadingList([rect], [{left:10,top:10,width:30,height:60}],914,410)).toBe(false);
    expect(needsReadingList([rect,{...rect,left:150}],[],914,410)).toBe(true);
    expect(needsReadingList([{...rect,left:10}], [{left:10,top:10,width:30,height:60}],914,410)).toBe(true);
    expect(needsReadingList([{...rect,top:400}],[],914,410)).toBe(true);
  });
});

describe('actual local batch arrival identity', () => {
  it('seeds saved data silently and never replays initial/recovery/hidden snapshots', () => {
    let tracker=acceptLocalSnapshot(createBatchArrivalTracker(),[target()],now);
    expect(tracker.arrivals.size).toBe(0);
    const next=target({observations:[observation({observed_at:new Date(now+1000).toISOString()})]});
    tracker=acceptLocalSnapshot(tracker,[next],now+1000);expect(tracker.arrivals.size).toBe(1);
    tracker=acceptLocalSnapshot(markDeliveryUnavailable(tracker),[next],now+2000);expect(tracker.arrivals.size).toBe(0);
    tracker=acceptLocalSnapshot(tracker,[target({observations:[observation({observed_at:new Date(now+3000).toISOString()})]})],now+3000,false);expect(tracker.arrivals.size).toBe(0);
    tracker=acceptLocalSnapshot(tracker,[target({observations:[observation({observed_at:new Date(now+3000).toISOString()})]})],now+4000,true);expect(tracker.arrivals.size).toBe(0);
  });
  it('seeds a changed revision independently and prunes withdrawn identity keys', () => {
    let tracker=acceptLocalSnapshot(createBatchArrivalTracker(),[target()],now);
    const revision=target({revision:2,observations:[observation({observed_at:new Date(now+1000).toISOString()})]});
    tracker=acceptLocalSnapshot(tracker,[revision],now+1000);
    expect(tracker.arrivals.size).toBe(0);expect(tracker.latestByKey.has(batchArrivalKey('fixture','ipv4','icmp',1))).toBe(false);
    expect(tracker.latestByKey.has(batchArrivalKey('fixture','ipv4','icmp',2))).toBe(true);
  });
});
