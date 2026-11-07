import { session } from "./api";
import { navigate, query, useLocation } from "./router";
import { homeFor, Landing, Login, Signup } from "./pages/Auth";
import { CreatorGig, CreatorHome, Onboard } from "./pages/Creator";
import { Booking, Bookings, PayLink } from "./pages/Client";

// Each role only ever sees its own screens.
export default function App() {
  const { path, search } = useLocation();
  const s = session.get();
  const role = s?.user.role;

  const pay = path.match(/^\/pay\/([^/]+)$/);
  if (pay) return <PayLink id={pay[1]} token={query(search, "t")} />;
  if (path === "/signup") return <Signup />;
  if (path === "/login") return <Login />;

  if (!s) {
    if (path !== "/") queueMicrotask(() => navigate(`/login?next=${encodeURIComponent(path + search)}`));
    return <Landing />;
  }

  if (role === "creator") {
    if (path === "/onboard") return <Onboard />;
    if (path === "/app") return <CreatorHome />;
    const g = path.match(/^\/app\/gigs\/([^/]+)$/);
    if (g) return <CreatorGig id={g[1]} />;
  }
  if (role === "client") {
    if (path === "/bookings") return <Bookings />;
    const b = path.match(/^\/bookings\/([^/]+)$/);
    if (b) return <Booking id={b[1]} />;
  }
  // Anything else (including the other role's screens) goes home.
  queueMicrotask(() => navigate(homeFor(role!, !!s.user.worker_id)));
  return null;
}
