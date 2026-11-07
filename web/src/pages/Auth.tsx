import { useState } from "react";
import { api, session, type Role } from "../api";
import { navigate, query, useLocation } from "../router";
import { markLime } from "../art";
import { Art, Strip } from "../brand";
import { Btn, ErrorNote, Field, RoundButton, Screen, Seg, ThemeToggle, useAction } from "../ui";

export function homeFor(role: Role, hasWorker: boolean) {
  if (role === "client") return "/bookings";
  return hasWorker ? "/app" : "/onboard";
}

function AuthHero({ line1, line2 }: { line1: string; line2: string }) {
  return (
    <div className="relative flex h-[230px] flex-col justify-end overflow-hidden rounded-[26px] bg-dg p-5">
      <Strip w={170} h={230} />
      <div className="absolute left-5 top-5"><Art svg={markLime} size={88} /></div>
      <div className="absolute right-4 top-4"><ThemeToggle /></div>
      <h1 className="relative text-[26px] font-extrabold leading-[1.15] tracking-[-0.02em] text-white">
        {line1}<br /><span className="text-lime">{line2}</span>
      </h1>
    </div>
  );
}

export function Landing() {
  return <Signup />;
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
    <Screen bottom={<>
      <Btn onClick={submit} disabled={busy || !f.name || !f.email || f.password.length < 6}>Create account</Btn>
      <p className="text-sm text-mu">Have an account?{" "}
        <button className="font-extrabold text-acc underline" onClick={() => navigate(`/login${next ? `?next=${encodeURIComponent(next)}` : ""}`)}>Log in</button>
      </p>
    </>}>
      <AuthHero line1={role === "creator" ? "Get paid before the gig." : "Book creators. Pay safely."} line2={role === "creator" ? "Safely." : "Held till it's done."} />
      <Seg value={role} onChange={setRole} options={[{ value: "creator", label: "I do gigs" }, { value: "client", label: "I hire" }]} />
      <div className="flex flex-col gap-3">
        <Field label="Your name" value={f.name} onChange={(v) => setF({ ...f, name: v })} placeholder="Abdullah Oyetoro" />
        <Field label="Email" value={f.email} onChange={(v) => setF({ ...f, email: v })} inputMode="email" placeholder="you@mail.com" />
        <Field label="Phone" optional value={f.phone} onChange={(v) => setF({ ...f, phone: v })} inputMode="tel" placeholder="+234…" />
        <Field label="Password" type="password" value={f.password} onChange={(v) => setF({ ...f, password: v })} placeholder="At least 6 characters" />
      </div>
      <ErrorNote msg={err} />
      <p className="text-xs text-mu">Demo logins (password demo1234): abdullah@demo.gigvault (creator), aminat@demo.gigvault (client).</p>
    </Screen>
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
    <Screen bottom={<>
      <Btn onClick={submit} disabled={busy || !email || !password}>Log in</Btn>
      <p className="text-sm text-mu">New here?{" "}
        <button className="font-extrabold text-acc underline" onClick={() => navigate(`/signup${next ? `?role=client&next=${encodeURIComponent(next)}` : ""}`)}>Create an account</button>
      </p>
    </>}>
      <AuthHero line1="Welcome back." line2="Your money's safe." />
      <div className="flex flex-col gap-3">
        <Field label="Email" value={email} onChange={setEmail} inputMode="email" />
        <Field label="Password" type="password" value={password} onChange={setPassword} />
      </div>
      <ErrorNote msg={err} />
      <p className="text-xs text-mu">Demo logins (password demo1234): abdullah@demo.gigvault (creator), aminat@demo.gigvault (client).</p>
    </Screen>
  );
}

export function LogoutButton() {
  return <RoundButton icon="logout" label="Log out" onClick={() => { session.clear(); navigate("/"); }} />;
}
