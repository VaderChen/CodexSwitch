const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const html=fs.readFileSync(require('node:path').join(__dirname,'../web/index.html'),'utf8');
function setup(backend){
 const elements={};
 for(const id of ['message','count','empty','accountList','dialog','dialogTitle','dialogText','dialogOK','dialogCancel'])elements[id]={hidden:true,style:{}};
 const global={document:{getElementById:id=>elements[id]},useAccount:backend,getAccounts:async()=> '[]',setTimeout};
 // In WebView, window IS the global object. A separate window mock misses collisions.
 global.window=global;
 const context=vm.createContext(global);
 vm.runInContext(html.match(/<script>([\s\S]*?)<\/script>/)[1],context);
 assert.equal(context.useAccount,backend,'page must not overwrite the Go binding');
 return {elements,run:()=>vm.runInContext("handleApplyAccount('test-account')",context)};
}
const flush=()=>new Promise(r=>setImmediate(r));
test('confirmation closes; backend called once; success dialog dismisses',async()=>{
 let calls=0,finish;
 const h=setup(id=>{assert.equal(id,'test-account');calls++;return new Promise(r=>finish=r)});
 const pending=h.run();
 assert.equal(calls,0);
 h.elements.dialogOK.onclick();
 assert.equal(h.elements.dialog.hidden,true);
 await flush();assert.equal(calls,1);
 await h.run();assert.equal(calls,1);
 finish('{"email":"test@example.com"}');await flush();
 assert.equal(h.elements.dialogTitle.textContent,'套用完成');
 assert.match(h.elements.dialogText.textContent,/自行重新啟動/);
 assert.equal(h.elements.dialogCancel.hidden,true);
 h.elements.dialogOK.onclick();await pending;
 assert.equal(h.elements.dialog.hidden,true);
});
test('cancel does not call backend',async()=>{
 let calls=0;const h=setup(async()=>{calls++});const pending=h.run();h.elements.dialogCancel.onclick();await pending;assert.equal(calls,0);assert.equal(h.elements.dialog.hidden,true);
});
test('backend error shows dismissible failure dialog',async()=>{
 const h=setup(async()=>'{"error":"test failure"}');const pending=h.run();h.elements.dialogOK.onclick();await flush();assert.equal(h.elements.dialogTitle.textContent,'套用失敗');assert.equal(h.elements.dialogText.textContent,'test failure');h.elements.dialogOK.onclick();await pending;assert.equal(h.elements.dialog.hidden,true);
});
