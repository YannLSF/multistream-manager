const $ = s => document.querySelector(s);
(async()=>{
  try{
    const r=await fetch('/api/auth/status',{cache:'no-store'});
    const s=await r.json();
    if(!s.enabled || s.authenticated){ location.replace('/'); return; }
    if(s.username) $('#loginUsername').value=s.username;
  }catch(_){ }
})();
$('#loginForm').addEventListener('submit',async e=>{
  e.preventDefault();
  const err=$('#loginError');
  err.classList.add('hidden');
  try{
    const r=await fetch('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:$('#loginUsername').value,password:$('#loginPassword').value})});
    const data=await r.json().catch(()=>({}));
    if(!r.ok) throw new Error(data.error||`HTTP ${r.status}`);
    location.replace('/');
  }catch(e){ err.textContent=e.message; err.classList.remove('hidden'); }
});
