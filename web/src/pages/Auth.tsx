import { useState } from "react";
import { api, session, type Role } from "../api";
import { navigate, query, useLocation } from "../router";
import { Button, ErrorNote, Field, Shell, useAction } from "../ui";

export function homeFor(role: Role, hasWorker: boolean) {
  if (role === "client") return "/bookings";
  return hasWorker ? "/app" : "/onboard";
}

export function Landing() {
  return (
    <Shell title="Get paid for every gig. Guaranteed." subtitle="Clients pay upfront. We hold it safely on Busha. You get paid the moment the gig is done.">
      <div className="grid gap-3">
        <Button onClick={() => navigate("/signup?role=creator")}>I'm a creator</Button>
        <Button variant="client" onClick={() => navigate("/signup?role=client")}>I'm booking a creator</Button>
        <Button variant="ghost" onClick={() => navigate("/login")}>Log in</Button>
      </div>
      <p className="text-xs text-stone-500">Demo logins (password demo1234): chidinma@demo.gigvault (creator), tolu@demo.gigvault (client).</p>
    </Shell>
  );
}

export function Signup() {
  const { search } = useLocation();
  const [role, setRole] = useState<Role>((query(search, "role") as Role) || "creator");
  const next = query(search, "next");
  const [f, setF] = useState({ name: "", email: "", phone: "", password: "" });
  const { busy, err, run } = useAction();

  const submit = () => run(async () => {
    const s = await api.signup({ ...f, role });
    session.set(s);
    navigate(next || homeFor(s.user.role, !!s.user.worker_id));
  });

  return (
    <Shell tone={role} title={role === "creator" ? "Create your creator account" : "Create your client account"}
      subtitle={role === "creator" ? "Get booked and paid safely." : "Book creators. Your money is protected until the gig is done."}>
      <div className="grid grid-cols-2 gap-1 rounded-lg bg-stone-100 p-1 text-sm">
        {(["creator", "client"] as Role[]).map((r) => (
          <button key={r} onClick={() => setRole(r)}
            className={`rounded-md py-1.5 font-medium capitalize ${role === r ? "bg-white shadow" : "text-stone-500"}`}>{r}</button>
        ))}
      </div>
      <Field label="Full name" value={f.name} onChange={(v) => setF({ ...f, name: v })} />
      <Field label="Email" value={f.email} onChange={(v) => setF({ ...f, email: v })} inputMode="email" />
      <Field label="Phone" value={f.phone} onChange={(v) => setF({ ...f, phone: v })} inputMode="tel" placeholder="+234…" />
      <Field label="Password" type="password" value={f.password} onChange={(v) => setF({ ...f, password: v })} />
      <ErrorNote msg={err} />
      <Button variant={role === "client" ? "client" : "primary"} onClick={submit} disabled={busy}>Create account</Button>
      <button className="w-full text-sm text-stone-500" onClick={() => navigate(`/login${next ? `?next=${encodeURIComponent(next)}` : ""}`)}>
        Already have an account? Log in
      </button>
    </Shell>
  );
}

export function Login() {
  const { search } = useLocation();
  const next = query(search, "next");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { busy, err, run } = useAction();
  const submit = () => run(async () => {
    const s = await api.login(email, password);
    session.set(s);
    navigate(next || homeFor(s.user.role, !!s.user.worker_id));
  });
  return (
    <Shell title="Welcome back">
      <Field label="Email" value={email} onChange={setEmail} inputMode="email" />
      <Field label="Password" type="password" value={password} onChange={setPassword} />
      <ErrorNote msg={err} />
      <Button onClick={submit} disabled={busy}>Log in</Button>
      <button className="w-full text-sm text-stone-500" onClick={() => navigate(`/signup${next ? `?role=client&next=${encodeURIComponent(next)}` : ""}`)}>
        New here? Create an account
      </button>
    </Shell>
  );
}

export function LogoutButton() {
  return (
    <button className="text-xs text-white/70 underline" onClick={() => { session.clear(); navigate("/"); }}>Log out</button>
  );
}
