import {metrics, type Metric} from './catalog.ts';
import type {Target} from './types';

export type InfrastructureKind='application'|'database'|'cache'|'frontend'|'probe'|'host'|'monitoring'|'other';
export const infrastructureLabels:Record<InfrastructureKind,string>={application:'애플리케이션',database:'데이터베이스',cache:'캐시',frontend:'프런트 서버',probe:'가용성 검사',host:'호스트',monitoring:'모니터링',other:'기타 인프라'};
export function infrastructureKind(target:Target):InfrastructureKind {
 const job=target.job.toLowerCase();
 if(/postgres|mysql|mariadb|oracle|mssql|mongodb|database/.test(job))return 'database';
 if(/redis|memcached|cache/.test(job))return 'cache';
 if(/nginx|frontend|gateway/.test(job))return 'frontend';
 if(/blackbox|probe/.test(job))return 'probe';
 if(/prometheus|alertmanager|grafana/.test(job))return 'monitoring';
 if(/node|windows|host/.test(job))return 'host';
 if(/application|app|api/.test(job))return 'application';
 return 'other';
}
export function infrastructureName(target:Target){
 const kind=infrastructureKind(target);
 return ({application:'Application server',database:/postgres/i.test(target.job)?'PostgreSQL':target.job,cache:/redis/i.test(target.job)?'Redis':target.job,frontend:/frontend|nginx/i.test(target.job)?'Nginx':target.job,probe:'HTTP health check',host:target.job,monitoring:target.job==='prometheus'?'Prometheus':target.job,other:target.job})[kind];
}
const primary:Record<InfrastructureKind,string[]>={application:['requests','p99','errors','memory'],database:['db-up','db-connections','db-buffer','db-rollback'],cache:['redis-up','redis-memory','redis-hit','redis-blocked'],frontend:['nginx-connections','scrape-duration','scrape-age','targets-down'],probe:['probe','probe-latency','dns','tls-expiry'],host:['node-cpu','memory-host','load','network-in'],monitoring:['scrape-duration','scrape-age','targets-down','alert-count'],other:['scrape-duration','scrape-age','targets-down']};
export const primaryMetricIds=(kind:InfrastructureKind)=>primary[kind];
export function infrastructureMetrics(kind:InfrastructureKind):readonly Metric[]{
 const sources:Record<InfrastructureKind,string[]>={application:['Application','Process collector','Session exporter','Credential exporter','Node.js exporter'],database:['Postgres exporter'],cache:['Redis exporter'],frontend:['Nginx exporter','Gateway instrumentation'],probe:['Blackbox exporter'],host:['Node exporter','cAdvisor','kube-state-metrics'],monitoring:[],other:[]};
 if(kind==='other')return metrics;
 return metrics.filter(m=>sources[kind].includes(m.source)||m.source==='Prometheus');
}
