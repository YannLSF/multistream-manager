const $ = s => document.querySelector(s);

function loginNext(){
  const raw = new URLSearchParams(location.search).get('next');

  if(!raw || !raw.startsWith('/') || raw.startsWith('//')){
    return '/';
  }

  try{
    const u = new URL(raw, location.origin);

    if(u.origin !== location.origin){
      return '/';
    }

    return u.pathname + u.search + u.hash;
  }catch(_){
    return '/';
  }
}

(async()=>{
  try{
    const r=await fetch('/api/auth/status',{cache:'no-store'});
    const s=await r.json();

    if(!s.enabled || s.authenticated){
      location.replace(loginNext());
      return;
    }

    if(s.username) $('#loginUsername').value=s.username;
  }catch(_){}
})();

$('#loginForm').addEventListener('submit',async e=>{
  e.preventDefault();

  const err=$('#loginError');
  err.classList.add('hidden');

  try{
    const r=await fetch('/api/auth/login',{
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({
        username:$('#loginUsername').value,
        password:$('#loginPassword').value
      })
    });

    const data=await r.json().catch(()=>({}));

    if(!r.ok){
      throw new Error(data.error||`HTTP ${r.status}`);
    }

    location.replace(loginNext());
  }catch(e){
    err.textContent=e.message;
    err.classList.remove('hidden');
  }
});
