const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function setup(){
 const elements={};for(const id of ['aboutVersion','appVersion','updateStatus','updateProgress','aboutDialog','updateButton','forceUpdateButton','updateNotice','updateNoticeText','downloadProgress'])elements[id]={textContent:'',hidden:true,style:{},dataset:{},classList:{toggle(){}},querySelector(){return {style:{}}},removeAttribute(){}};
 let calls=0,polling=0,state={phase:'checking'};
 const c=vm.createContext({document:{getElementById:id=>elements[id],querySelectorAll:()=>[]},window:{checkForUpdate:async()=>{calls++},getUpdateStatus:async()=>state},setInterval:()=>++polling,clearInterval(){}});
 const html=fs.readFileSync(require('node:path').join(__dirname,'../web/index.html'),'utf8');
 const script=html.slice(html.indexOf('let activeModal='),html.indexOf('function setMessage('));
 vm.runInContext('const $=id=>document.getElementById(id);'+script,c);
 return {c,e:elements,state:s=>state=s,calls:()=>calls,polling:()=>polling};
}
test('啟動只檢查一次，有新版顯示通知',async()=>{
 const h=setup();await vm.runInContext('checkStartupUpdate()',h.c);await vm.runInContext('checkStartupUpdate()',h.c);assert.equal(h.calls(),1);
 h.state({phase:'available',version:'1.26.0919 build 1200'});await vm.runInContext('refreshUpdateStatus()',h.c);
 assert.equal(h.e.updateNotice.hidden,false);assert.match(h.e.updateNoticeText.textContent,/1.26.0919/);assert.equal(h.e.updateButton.dataset.install,'true');
});
test('下載百分比在主畫面顯示，重開關於恢復輪詢',async()=>{
 const h=setup();h.state({phase:'downloading',downloaded:524288,total:1048576});await vm.runInContext('refreshUpdateStatus()',h.c);
 assert.equal(h.e.downloadProgress.value,50);assert.equal(h.e.downloadProgress.hidden,false);assert.match(h.e.updateNoticeText.textContent,/50%/);
 vm.runInContext('openAbout()',h.c);assert.equal(h.polling(),1);
 await new Promise(r=>setImmediate(r));
 h.state({phase:'current',message:'目前已是最新版本。'});await vm.runInContext('refreshUpdateStatus()',h.c);assert.equal(h.e.updateNotice.hidden,true);
});

test('強制更新直接呼叫安裝流程並暫停兩個按鈕',async()=>{
 const h=setup();let calls=0;h.c.window.forceUpdate=async()=>{calls++};
 await vm.runInContext('handleForceUpdate()',h.c);
 assert.equal(calls,1);assert.equal(h.calls(),0);assert.equal(h.e.forceUpdateButton.disabled,true);assert.equal(h.e.updateButton.disabled,true);
 await vm.runInContext('handleForceUpdate()',h.c);assert.equal(calls,1);
});
