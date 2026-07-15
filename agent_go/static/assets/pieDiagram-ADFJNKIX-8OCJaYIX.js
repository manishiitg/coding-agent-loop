import{_ as p,g as j,s as Z,a as q,b as H,p as J,o as K,l as F,c as Q,C as X,G as Y,N as ee,d as te,x as ae,E as re}from"./mermaid.core-CBvdfUcq.js";import{p as ne}from"./chunk-4BX2VUAB-DWZoVjYf.js";import{p as ie}from"./treemap-KMMF4GRG-DdqGkhoy.js";import"./index-DUqqK3bz.js";import{d as I}from"./arc-CRtpYiTb.js";import{o as se}from"./ordinal-DILIJJjt.js";import{b as S,t as z,n as oe}from"./step-COwQ4PBn.js";import"./min-kVaLYba5.js";import"./_baseUniq-BS5Up_c2.js";import"./init-Dmth1JHB.js";function le(e,a){return a<e?-1:a>e?1:a>=e?0:NaN}function ce(e){return e}function ue(){var e=ce,a=le,f=null,x=S(0),s=S(z),l=S(0);function o(t){var n,c=(t=oe(t)).length,d,y,h=0,u=new Array(c),i=new Array(c),v=+x.apply(this,arguments),w=Math.min(z,Math.max(-z,s.apply(this,arguments)-v)),m,D=Math.min(Math.abs(w)/c,l.apply(this,arguments)),$=D*(w<0?-1:1),g;for(n=0;n<c;++n)(g=i[u[n]=n]=+e(t[n],n,t))>0&&(h+=g);for(a!=null?u.sort(function(A,C){return a(i[A],i[C])}):f!=null&&u.sort(function(A,C){return f(t[A],t[C])}),n=0,y=h?(w-c*$)/h:0;n<c;++n,v=m)d=u[n],g=i[d],m=v+(g>0?g*y:0)+$,i[d]={data:t[d],index:n,value:g,startAngle:v,endAngle:m,padAngle:D};return i}return o.value=function(t){return arguments.length?(e=typeof t=="function"?t:S(+t),o):e},o.sortValues=function(t){return arguments.length?(a=t,f=null,o):a},o.sort=function(t){return arguments.length?(f=t,a=null,o):f},o.startAngle=function(t){return arguments.length?(x=typeof t=="function"?t:S(+t),o):x},o.endAngle=function(t){return arguments.length?(s=typeof t=="function"?t:S(+t),o):s},o.padAngle=function(t){return arguments.length?(l=typeof t=="function"?t:S(+t),o):l},o}var pe=re.pie,G={sections:new Map,showData:!1},T=G.sections,N=G.showData,de=structuredClone(pe),ge=p(()=>structuredClone(de),"getConfig"),fe=p(()=>{T=new Map,N=G.showData,ae()},"clear"),me=p(({label:e,value:a})=>{if(a<0)throw new Error(`"${e}" has invalid value: ${a}. Negative values are not allowed in pie charts. All slice values must be >= 0.`);T.has(e)||(T.set(e,a),F.debug(`added new section: ${e}, with value: ${a}`))},"addSection"),he=p(()=>T,"getSections"),ve=p(e=>{N=e},"setShowData"),Se=p(()=>N,"getShowData"),L={getConfig:ge,clear:fe,setDiagramTitle:K,getDiagramTitle:J,setAccTitle:H,getAccTitle:q,setAccDescription:Z,getAccDescription:j,addSection:me,getSections:he,setShowData:ve,getShowData:Se},xe=p((e,a)=>{ne(e,a),a.setShowData(e.showData),e.sections.map(a.addSection)},"populateDb"),ye={parse:p(async e=>{const a=await ie("pie",e);F.debug(a),xe(a,L)},"parse")},we=p(e=>`
  .pieCircle{
    stroke: ${e.pieStrokeColor};
    stroke-width : ${e.pieStrokeWidth};
    opacity : ${e.pieOpacity};
  }
  .pieOuterCircle{
    stroke: ${e.pieOuterStrokeColor};
    stroke-width: ${e.pieOuterStrokeWidth};
    fill: none;
  }
  .pieTitleText {
    text-anchor: middle;
    font-size: ${e.pieTitleTextSize};
    fill: ${e.pieTitleTextColor};
    font-family: ${e.fontFamily};
  }
  .slice {
    font-family: ${e.fontFamily};
    fill: ${e.pieSectionTextColor};
    font-size:${e.pieSectionTextSize};
    // fill: white;
  }
  .legend text {
    fill: ${e.pieLegendTextColor};
    font-family: ${e.fontFamily};
    font-size: ${e.pieLegendTextSize};
  }
`,"getStyles"),Ae=we,Ce=p(e=>{const a=[...e.values()].reduce((s,l)=>s+l,0),f=[...e.entries()].map(([s,l])=>({label:s,value:l})).filter(s=>s.value/a*100>=1).sort((s,l)=>l.value-s.value);return ue().value(s=>s.value)(f)},"createPieArcs"),De=p((e,a,f,x)=>{F.debug(`rendering pie chart
`+e);const s=x.db,l=Q(),o=X(s.getConfig(),l.pie),t=40,n=18,c=4,d=450,y=d,h=Y(a),u=h.append("g");u.attr("transform","translate("+y/2+","+d/2+")");const{themeVariables:i}=l;let[v]=ee(i.pieOuterStrokeWidth);v??=2;const w=o.textPosition,m=Math.min(y,d)/2-t,D=I().innerRadius(0).outerRadius(m),$=I().innerRadius(m*w).outerRadius(m*w);u.append("circle").attr("cx",0).attr("cy",0).attr("r",m+v/2).attr("class","pieOuterCircle");const g=s.getSections(),A=Ce(g),C=[i.pie1,i.pie2,i.pie3,i.pie4,i.pie5,i.pie6,i.pie7,i.pie8,i.pie9,i.pie10,i.pie11,i.pie12];let b=0;g.forEach(r=>{b+=r});const W=A.filter(r=>(r.data.value/b*100).toFixed(0)!=="0"),E=se(C);u.selectAll("mySlices").data(W).enter().append("path").attr("d",D).attr("fill",r=>E(r.data.label)).attr("class","pieCircle"),u.selectAll("mySlices").data(W).enter().append("text").text(r=>(r.data.value/b*100).toFixed(0)+"%").attr("transform",r=>"translate("+$.centroid(r)+")").style("text-anchor","middle").attr("class","slice"),u.append("text").text(s.getDiagramTitle()).attr("x",0).attr("y",-400/2).attr("class","pieTitleText");const O=[...g.entries()].map(([r,M])=>({label:r,value:M})),k=u.selectAll(".legend").data(O).enter().append("g").attr("class","legend").attr("transform",(r,M)=>{const R=n+c,B=R*O.length/2,V=12*n,U=M*R-B;return"translate("+V+","+U+")"});k.append("rect").attr("width",n).attr("height",n).style("fill",r=>E(r.label)).style("stroke",r=>E(r.label)),k.append("text").attr("x",n+c).attr("y",n-c).text(r=>s.getShowData()?`${r.label} [${r.value}]`:r.label);const _=Math.max(...k.selectAll("text").nodes().map(r=>r?.getBoundingClientRect().width??0)),P=y+t+n+c+_;h.attr("viewBox",`0 0 ${P} ${d}`),te(h,d,P,o.useMaxWidth)},"draw"),$e={draw:De},Oe={parser:ye,db:L,renderer:$e,styles:Ae};export{Oe as diagram};
