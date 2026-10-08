export type Sample={time:number;value:number|null};
export type MetricSeries={labels:Record<string,string>;points:Sample[]};
export type MetricResult={id:string;state:'ok'|'missing'|'error'|'stale';latest:number|null;series:MetricSeries[];truncated?:boolean;message?:string};
export type Target={instance:string;job:string;health:string;lastScrape:string;lastError?:string};
export type Alert={name:string;state:string;severity:string;instance:string;activeAt:string;summary:string};
export type AlertHistory={name:string;instance:string;severity:string;startedAt:number;endedAt:number|null;resolutionSeconds:number};
export type Snapshot={mode:'test'|'production'|'unconfigured';connected:boolean;collectedAt:string;start:number;end:number;step:number;metrics:MetricResult[];targets:Target[];alerts:Alert[];history?:AlertHistory[];grafanaUrl:string|null;message?:string};
export type Incident={id:string;title:string;severity:'critical'|'warning'|'notice';kind:'detected'|'predicted'|'expiring'|'collection';summary:string;metricIds:string[];evidence:string[];steps:string[];value:string;unit:string;firstSeen?:string;status:'firing'|'pending';scope:string};
