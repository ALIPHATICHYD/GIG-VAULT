import { session } from "./api";
import { navigate, query, useLocation } from "./router";
import { homeFor, Landing, Login, Signup } from "./pages/Auth";
import { CashOut, CreatorGig, CreatorHome, Invoices, Me, NewInvoice, Onboard } from "./pages/Creator";
import { Booking, Bookings, CancelBooking, ClientMe, PayLink } from "./pages/Client";
import { Admin } from "./pages/Admin";

// Each role only ever sees its own screens.
export default function App() {
  const { path, search } = useLocation();
  const s = session.get();
  const role = s?.user.role;

  const pay = path.match(/^\/pay\/([^/]+)$/);
  if (pay) return <PayLink id={pay[1]} token={query(search, "t")} />;
  if (path === "/signup") return <Signup />;
  if (path === "/login") return <Login />;
  if (path === "/admin") return <Admin />;

  if (!s) {
    if (path !== "/") queueMicrotask(() => navigate(`/login?next=${encodeURIComponent(path + search)}`));
    return <Landing />;
  }

  if (role === "creator") {
    if (path === "/onboard") return <Onboard />;
    if (path === "/app") return <CreatorHome />;
    if (path === "/app/new") return <NewInvoice />;
    if (path === "/app/invoices") return <Invoices />;
    if (path === "/app/cashout") return <CashOut />;
    if (path === "/app/me") return <Me />;
    const g = path.match(/^\/app\/gigs\/([^/]+)$/);
    if (g) return <CreatorGig id={g[1]} />;
  }
  if (role === "client") {
    if (path === "/bookings") return <Bookings />;
    if (path === "/bookings/history") return <Bookings tab="history" />;
    if (path === "/bookings/me") return <ClientMe />;
    const c = path.match(/^\/bookings\/([^/]+)\/cancel$/);
    if (c) return <CancelBooking id={c[1]} />;
    const b = path.match(/^\/bookings\/([^/]+)$/);
    if (b) return <Booking id={b[1]} />;
  }
  // Anything else (including the other role's screens) goes home.
  queueMicrotask(() => navigate(homeFor(role!, !!s.user.worker_id)));
  return null;
}
