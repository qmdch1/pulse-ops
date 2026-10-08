// Persist identities and presentation settings only, never samples or credentials.
export const recipeKey=r=>JSON.stringify([r.title,[...r.assetIds].sort(),[...r.metricIds].sort(),[...(r.keys||[])].sort()]);
export function refreshPeriod(value){const n=Number(value);return Number.isInteger(n)&&n>=1&&n<=3600?n:null}
export function refreshDue(last,now,custom,base,force=false){const seconds=refreshPeriod(custom)??(refreshPeriod(base)||Infinity);return force||now-last>=seconds*1000}
export function readLayout(raw){return {groups:Array.isArray(raw?.groups)?raw.groups.filter(g=>Array.isArray(g)&&g.length>1&&g.length<=200&&g.every(k=>typeof k==='string'&&k.length<=20000)).slice(-100).map(g=>[...new Set(g)]):[],split:Array.isArray(raw?.split)?raw.split.filter(k=>typeof k==='string').slice(-200):[],periods:raw?.periods&&typeof raw.periods==='object'&&!Array.isArray(raw.periods)?Object.fromEntries(Object.entries(raw.periods).filter(([k,v])=>k.length<=20000&&refreshPeriod(v)).slice(-500)):{} }}
export function mergeLayout(groups,source,target){if(source===target)return groups;const a=groups.find(g=>g.includes(source))||[source],b=groups.find(g=>g.includes(target))||[target];if(a===b)return groups;return [...groups.filter(g=>g!==a&&g!==b),[...new Set([...b,...a])]].slice(-100)}
export function activeGroups(groups,available){const used=new Set(),result=[];for(const g of groups)if(g.length>1&&g.every(k=>available.has(k)&&!used.has(k))){g.forEach(k=>used.add(k));result.push(g)}return result}
export function removeGroup(groups,key){return groups.filter(g=>!g.includes(key))}
