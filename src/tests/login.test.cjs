const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const html=fs.readFileSync(require('node:path').join(__dirname,'../web/index.html'),'utf8');
function setup(startLogin) {
 const elements={};
 for(const id of ['email','loginButton','cancelLoginButton','message','count','empty','accountList']) elements[id]={value:'user@example.com',style:{},disabled:false,classList:new Set(),focus(){},checkValidity(){return true;}};
 elements.loginButton.classList.remove=elements.loginButton.classList.delete;
 const context=vm.createContext({document:{addEventListener(){},getElementById:id=>elements[id]||null},window:{startLogin,getAccounts:async()=> '[]'},setTimeout,clearTimeout,setInterval:()=>0});
 vm.runInContext(html.match(/<script>([\s\S]*?)<\/script>/)[1],context);
 return {elements,context,run:()=>vm.runInContext('login()',context)};
}
test('login opens backend without deleted header, and restores button',async()=>{
 let calls=0; const h=setup(async email=>{calls++;assert.equal(email,'user@example.com');return '{"account":{}}';});
 await h.run();assert.equal(calls,1);assert.equal(h.elements.loginButton.disabled,false);assert.equal(h.elements.loginButton.classList.has('loading'),false);
});
for(const [name,backend] of [['backend failure',async()=>{throw Error('browser failed')}],['invalid response',async()=>undefined],['missing bridge',undefined]]) {
 test(name+' restores controls',async()=>{const h=setup(backend);await h.run();assert.equal(h.elements.loginButton.disabled,false);assert.equal(h.elements.loginButton.classList.has('loading'),false);assert.match(h.elements.message.className,/error/);});
}

test('取消背景登入後恢復控制項，並允許重新登入',async()=>{
 const h=setup(async()=>'{"pending":true}');
 h.context.window.cancelLogin=async()=>{
  h.context.window.resolveLogin({cancelled:true});
  return '{"ok":true}';
 };
 for(let attempt=0;attempt<2;attempt++){
  const pending=h.run();
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(h.elements.loginButton.disabled,true);
  assert.equal(h.elements.cancelLoginButton.hidden,false);
  await vm.runInContext('handleCancelLogin()',h.context);
  await pending;
  assert.equal(h.elements.loginButton.disabled,false);
  assert.equal(h.elements.cancelLoginButton.hidden,true);
  assert.equal(h.elements.email.value,'user@example.com');
  assert.equal(h.elements.message.textContent,'已取消登入。');
 }
});
