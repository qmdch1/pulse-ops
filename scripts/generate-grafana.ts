import {writeFileSync} from 'node:fs';
import {metrics,scopedQuery,groupLabels} from '../lib/monitoring/catalog.ts';
let y=0;const panels:unknown[]=[];
for(const [group,label]of Object.entries(groupLabels)){
 panels.push({id:panels.length+1,type:'row',title:label,gridPos:{h:1,w:24,x:0,y:y++},collapsed:false});
 const list=metrics.filter(m=>m.group===group);
 list.forEach((m,i)=>panels.push({id:panels.length+1,type:'timeseries',title:m.title,description:m.description,gridPos:{h:8,w:12,x:(i%2)*12,y:y+Math.floor(i/2)*8},datasource:{type:'prometheus',uid:'pulse-prometheus'},targets:[{refId:'A',expr:scopedQuery(m.query),legendFormat:'{{instance}} {{version}}'}],fieldConfig:{defaults:{unit:m.unit==='ms'?'ms':m.unit==='%'?'percent':'short',custom:{lineWidth:2,fillOpacity:8},thresholds:{mode:'absolute',steps:[{color:'green',value:null},...(m.warning===undefined?[]:[{color:'orange',value:m.warning}])]}}},options:{legend:{displayMode:'list',placement:'bottom'},tooltip:{mode:'multi'}}}));
 y+=Math.ceil(list.length/2)*8;
}
panels.push({id:panels.length+1,type:'state-timeline',title:'경보 발생·해제 이력',gridPos:{x:0,y,w:24,h:8},datasource:{type:'prometheus',uid:'pulse-prometheus'},targets:[{refId:'A',expr:'ALERTS{alertstate="firing"}',legendFormat:'{{alertname}} · {{instance}}'}]});
writeFileSync('monitoring/grafana/dashboards/pulse-ops.json',JSON.stringify({uid:'pulse-ops',title:'PULSE / OPS · Infrastructure',schemaVersion:41,version:1,editable:false,refresh:'30s',timezone:'Asia/Seoul',time:{from:'now-1h',to:'now'},tags:['pulse-ops'],annotations:{list:[{name:'Deployments',datasource:{type:'prometheus',uid:'pulse-prometheus'},enable:true,iconColor:'#b5a1ff',expr:'changes(app_deployment_timestamp_seconds[1m]) > 0',titleFormat:'배포 {{version}}',useValueForTime:false}]},panels},null,2));
console.log(`Generated ${metrics.length} metric panels plus alert history`);

