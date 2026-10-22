package widget

// THE USER-AGENT SHEET — golib's defaults, under a host's sheet and the page's.

// uaCSS is golib's user-agent stylesheet, in the subset: what an unstyled page looks like.
const uaCSS = `
html,body,div,p,ul,ol,dl,dt,dd,pre,blockquote,figure,figcaption,section,article,aside,header,footer,nav,main,address,details,summary,h1,h2,h3,h4,h5,h6,hr,table,caption,center,fieldset,form,hgroup,menu,legend{display:block}
head,script,style,title,meta,link,template,noscript,base{display:none}
li{display:list-item}
table{display:table}
thead,tbody,tfoot{display:table-row-group}
tr{display:table-row}
td,th{display:table-cell;padding:2px 6px}
caption{text-align:center}
body{margin:8px}
p,ul,ol,dl,pre,blockquote,table,figure{margin:1em 0}
ul,ol,menu{padding-left:2em}
li ul,li ol{margin:0}
dd{margin-left:2em}
h1{font-size:2em;font-weight:bold;margin:.67em 0}
h2{font-size:1.5em;font-weight:bold;margin:.83em 0}
h3{font-size:1.17em;font-weight:bold;margin:1em 0}
h4{font-weight:bold;margin:1.33em 0}
h5{font-size:.83em;font-weight:bold;margin:1.67em 0}
h6{font-size:.67em;font-weight:bold;margin:2.33em 0}
b,strong,th,dt{font-weight:bold}
i,em,cite,dfn,var,address{font-style:italic}
pre,code,kbd,samp,tt{font-family:monospace}
pre{white-space:pre}
a,u,ins{text-decoration:underline}
s,del,strike{text-decoration:line-through}
blockquote{padding-left:1em;margin-left:0;margin-right:0}
hr{margin:.5em 0;border-top:1px solid}
.callout{margin:1em 0;padding:.5em 1em;border-left:4px solid}
.callout-title{font-weight:bold}
`
