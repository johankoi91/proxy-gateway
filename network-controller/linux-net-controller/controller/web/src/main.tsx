import React from "react";
import ReactDOM from "react-dom/client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Activity, CheckCircle2, CircleAlert, Database, Globe2, Network, Play, Power, PowerOff, RefreshCw, RotateCcw, Server, Settings2, Shield, Waypoints } from "lucide-react";
import "./styles.css";
import "./audit.css";

type Check={id:string;healthy:boolean;detail:string};
type Action={id:string;kind:string;status:string;phase:string;message:string};
type Audit={checkedAt:string;status:string;passed:string[];skipped:string[];remediations?:string[]};
type Configuration={managementAddress:string;physicalInterface:string;managementCidr:string;tunnelInterface:string;tunnelAddress:string;tunnelDns:string;vpsAddress:string;vpsPort:number;expectedEgress:string;provider:string;runtime:string};
type Snapshot={version:string;protected:boolean;connected:boolean;expectedEgress:string;observedEgress?:string;checks:Check[];checkedAt:string;providerState:string;mode:string;action?:Action;lastAudit?:Audit;configuration:Configuration};
type View="overview"|"checks"|"config";

const labels:Record<string,{name:string,description:string}>={
  physical_interface:{name:"北京主机网络",description:"北京 VPS 的物理网络接口"},
  kill_switch:{name:"精确断网策略",description:"拒绝额外物理放行，阻止主机、Docker 和 IPv6 回落"},
  boot_services:{name:"开机与持续保护",description:"开机加载 Kill Switch，并每 10 秒检查和修复"},
  sing_box_process:{name:"东京线路进程",description:"Docker 容器内的 sing-box"},
  tunnel:{name:"TUN 隧道",description:"承接北京主机与 Selkies Chrome 公网流量"},
  route_rule_hygiene:{name:"路由规则残留",description:"检测容器重建遗留的 sing-box 内核规则"},
  dns:{name:"DNS 配置与解析",description:"核对 172.19.0.2 并执行真实解析"},
  time_sync:{name:"时间同步路径",description:"核对 chrony 正常且绑定 TUN"},
  ipv6_leak:{name:"IPv6 路由与探针",description:"核对无公网默认路由且主动探针失败关闭"},
  tokyo_endpoint:{name:"东京物理 Endpoint",description:"从北京物理网络验证东京 TCP 443"},
  tokyo_egress:{name:"北京主机出口",description:"主动访问外部服务核对主机出口 IP"},
  docker_egress:{name:"Docker 业务出口",description:"从业务容器核对实际公网出口"}
};

const checkGroups=[
  {name:"物理边界与开机保护",ids:["physical_interface","kill_switch","boot_services","tokyo_endpoint"]},
  {name:"东京隧道与防泄漏",ids:["sing_box_process","tunnel","route_rule_hygiene","dns","time_sync","ipv6_leak"]},
  {name:"端到端出口",ids:["tokyo_egress","docker_egress"]}
];

function App(){
  const [snapshot,setSnapshot]=useState<Snapshot|null>(null); const [view,setView]=useState<View>("overview"); const [busy,setBusy]=useState(""); const [error,setError]=useState("");
  const load=useCallback(async()=>{const response=await fetch("/api/snapshot",{cache:"no-store"});if(!response.ok)throw new Error("状态读取失败");setSnapshot(await response.json())},[]);
  useEffect(()=>{void load().catch(e=>setError(e.message));if(view!=="overview")return;const timer=setInterval(()=>void load().catch(()=>{}),5000);return()=>clearInterval(timer)},[load,view]);
  const checks=snapshot?.checks||[]; const failures=useMemo(()=>checks.filter(c=>!c.healthy).length,[checks]);
  async function waitForFresh(previous?:string){for(let i=0;i<30;i++){await new Promise(r=>setTimeout(r,500));const response=await fetch("/api/snapshot",{cache:"no-store"});const next:Snapshot=await response.json();if(next.checkedAt!==previous){setSnapshot(next);return}}await load()}
  async function runCheck(){setBusy("check");setError("");try{const previous=snapshot?.checkedAt;const response=await fetch("/api/check",{method:"POST"});if(!response.ok)throw new Error("检查任务启动失败");await waitForFresh(previous)}catch(e){setError(e instanceof Error?e.message:"检查失败")}finally{setBusy("")}}
  async function action(kind:string){if(kind==="pause"&&!confirm("安全暂停东京线路？公网将被阻断，但管理后台和远程浏览器仍可访问。"))return;setBusy(kind);setError("");try{const previous=snapshot?.checkedAt;let response=await fetch(`/api/actions/${kind}`,{method:"POST"});let current:Action=await response.json();if(!response.ok)throw new Error((current as unknown as {error:string}).error||"操作失败");for(let i=0;i<60&&current.status==="running";i++){await new Promise(r=>setTimeout(r,500));response=await fetch(`/api/actions/${current.id}`,{cache:"no-store"});current=await response.json()}await waitForFresh(previous);if(current.status==="failed")throw new Error(current.message)}catch(e){setError(e instanceof Error?e.message:"操作失败")}finally{setBusy("")}}
  const healthy=Boolean(snapshot?.protected&&snapshot?.connected&&failures===0); const paused=snapshot?.providerState==="paused";
  return <div className="app"><header><div className="brand"><Shield size={22}/><span>LightNode Linux Controller</span></div><div className="actions"><button onClick={()=>void runCheck()} disabled={!!busy}><Play size={17}/>运行检查</button>{paused?<button onClick={()=>void action("start")} disabled={!!busy}><Power size={17}/>启动线路</button>:<button onClick={()=>void action("pause")} disabled={!!busy}><PowerOff size={17}/>安全暂停</button>}<button onClick={()=>void action("restart")} disabled={!!busy||paused}><RotateCcw size={17}/>重启线路</button></div></header>
  <nav><button className={view==="overview"?"active":""} onClick={()=>setView("overview")}><Activity size={18}/>总览</button><button className={view==="checks"?"active":""} onClick={()=>setView("checks")}><Shield size={18}/>系统检查{failures>0&&<b>{failures}</b>}</button><button className={view==="config"?"active":""} onClick={()=>setView("config")}><Settings2 size={18}/>实际配置</button></nav>
  <main>{error&&<div className="error"><CircleAlert size={19}/>{error}</div>}{view==="overview"&&<Overview snapshot={snapshot} healthy={healthy}/>} {view==="checks"&&<Checks snapshot={snapshot}/>} {view==="config"&&<Config snapshot={snapshot}/>}</main>
  {busy&&<div className="overlay"><div><RefreshCw className="spin" size={32}/><h2>{busy==="pause"?"正在安全暂停":busy==="start"?"正在启动东京线路":busy==="restart"?"正在重启东京线路":"正在读取真实状态"}</h2><p>{snapshot?.action?.message||"请稍候，完成后页面会自动更新"}</p></div></div>}</div>
}

function Overview({snapshot,healthy}:{snapshot:Snapshot|null;healthy:boolean}){const audit=snapshot?.lastAudit;const flowIds=["physical_interface","kill_switch","tokyo_endpoint","sing_box_process","tunnel","tokyo_egress","docker_egress"];return <><section className={`hero ${healthy?"ok":"locked"}`}><div><span className="eyebrow">北京 VPS · Docker 全局接管</span><h1>{healthy?"网络保护正常":snapshot?.protected?"已安全断网":"状态尚未就绪"}</h1><p>{healthy?"北京主机和 Docker 业务流量均已主动核对为东京出口。":"任一网络边界异常时，整体状态不会显示为正常。"}</p></div><div className="headline-ip"><small>当前公网出口</small><strong>{snapshot?.observedEgress||"不可用"}</strong><span>{formatTime(snapshot?.checkedAt)}</span></div></section><section className="metrics"><Metric icon={<Globe2/>} label="主机与 Docker 出口" value={snapshot?.connected?"均已验证":"不可用"} ok={!!snapshot?.connected}/><Metric icon={<Shield/>} label="精确断网策略" value={snapshot?.protected?"已验证":"异常"} ok={!!snapshot?.protected}/><Metric icon={<Waypoints/>} label="TUN 隧道" value={snapshot?.configuration?.tunnelInterface||"未建立"} ok={check(snapshot,"tunnel")}/><Metric icon={<Database/>} label="DNS 实际解析" value={check(snapshot,"dns")?"成功":"异常"} ok={check(snapshot,"dns")}/></section><section className="section"><div className="section-title"><h2>网络链路</h2><span>按数据流顺序</span></div><div className="flow">{flowIds.map((id,i)=><React.Fragment key={id}><FlowItem item={snapshot?.checks.find(c=>c.id===id)} id={id}/>{i<flowIds.length-1&&<span className="arrow">→</span>}</React.Fragment>)}</div></section><section className="audit-strip"><div><small>最近严格演练</small><strong>{audit?.status==="passed"?"受控故障测试通过":"尚无有效记录"}</strong></div><div><small>已验证</small><strong>{audit?.passed.length||0} 项</strong></div><div><small>已修复</small><strong>{audit?.remediations?.length||0} 项</strong></div><div><small>未执行</small><strong>{audit?.skipped.join("、")||"无"}</strong></div><time>{audit?formatDateTime(audit.checkedAt):"-"}</time></section></>}
function Checks({snapshot}:{snapshot:Snapshot|null}){return <section className="section"><div className="section-title"><div><h2>系统检查</h2><p>仅检查网络接管、防泄漏、恢复能力和端到端出口</p></div><span>{formatTime(snapshot?.checkedAt)}</span></div><div className="check-list">{checkGroups.map(group=><section className="check-group" key={group.name}><h3>{group.name}</h3>{group.ids.map(id=>snapshot?.checks.find(item=>item.id===id)).filter((item):item is Check=>Boolean(item)).map(item=>{const text=labels[item.id]||{name:item.id,description:"运行状态"};return <div className="check-row" key={item.id}><span className={item.healthy?"status good":"status bad"}>{item.healthy?<CheckCircle2/>:<CircleAlert/>}</span><div><strong>{text.name}</strong><small>{text.description}</small></div><code>{friendly(item)}</code><b className={item.healthy?"good-text":"bad-text"}>{item.healthy?"正常":"异常"}</b></div>})}</section>)}</div></section>}
function Config({snapshot}:{snapshot:Snapshot|null}){const c=snapshot?.configuration;if(!c)return <section className="section">配置尚未就绪</section>;const groups=[{title:"北京主机入口",icon:<Network/>,rows:[["管理地址",`${c.managementAddress}:9180`],["物理接口",c.physicalInterface],["管理网段",c.managementCidr]]},{title:"东京隧道",icon:<Waypoints/>,rows:[["TUN 接口",c.tunnelInterface],["TUN 地址",c.tunnelAddress],["隧道 DNS",c.tunnelDns]]},{title:"固定出口",icon:<Server/>,rows:[["VPS Endpoint",`${c.vpsAddress}:${c.vpsPort}`],["期望公网 IP",c.expectedEgress],["传输组件",c.provider]]},{title:"运行方式",icon:<Settings2/>,rows:[["Controller 版本",snapshot.version],["运行环境",c.runtime],["容器", "lightnode-linux-controller"],["配置挂载","只读"]]}];return <section className="section"><div className="section-title"><div><h2>实际配置</h2><p>只展示运行中的非敏感参数，不回显 UUID、Reality 密钥或 Short ID</p></div></div><div className="config-groups">{groups.map(group=><div className="config-group" key={group.title}><h3>{group.icon}{group.title}</h3>{group.rows.map(row=><div className="config-row" key={row[0]}><span>{row[0]}</span><code>{row[1]}</code></div>)}</div>)}</div></section>}
function Metric({icon,label,value,ok}:{icon:React.ReactNode;label:string;value:string;ok:boolean}){return <div className="metric"><span>{icon}</span><div><small>{label}</small><strong>{value}</strong></div><i className={ok?"dot good":"dot bad"}/></div>}
function FlowItem({item,id}:{item?:Check;id:string}){return <div className={item?.healthy?"flow-item good-border":"flow-item bad-border"}><span>{item?.healthy?<CheckCircle2/>:<CircleAlert/>}</span><small>{labels[id]?.name||id}</small><strong>{item?.healthy?"正常":"异常"}</strong></div>}
function check(s:Snapshot|null,id:string){return !!s?.checks.find(c=>c.id===id)?.healthy}
function friendly(item:Check){if(item.id==="kill_switch"&&item.healthy)return "规则与允许列表完全一致";if(item.id==="boot_services"&&item.healthy)return "开机加载 · 10 秒自愈";if(item.id==="dns"&&item.healthy)return "172.19.0.2 · 解析成功";if(item.id==="time_sync"&&item.healthy)return "已同步 · 绑定 TUN";if(item.id==="ipv6_leak"&&item.healthy)return "无公网路由 · 主动探针阻断";if(item.id==="sing_box_process")return item.healthy?"单实例受控运行":item.detail;if(item.id==="route_rule_hygiene")return item.healthy?"mark 0x2023 → table 2022":item.detail;return item.detail||"-"}
function formatTime(v?:string){if(!v)return "尚未检查";return new Date(v).toLocaleTimeString("zh-CN",{hour12:false})}
function formatDateTime(v:string){return new Intl.DateTimeFormat("zh-CN",{month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hour12:false}).format(new Date(v))}

ReactDOM.createRoot(document.getElementById("root")!).render(<React.StrictMode><App/></React.StrictMode>);
