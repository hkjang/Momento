import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { APIError, get, post, type User } from "../api/client";

interface AuthValue {
  user: User | null;
  loading: boolean;
  /**
   * Set when the console could not find out whether anybody is signed in.
   *
   * Any failure used to mean "not signed in": a network blip, a 500, a timeout
   * all cleared the user and sent the reader to the login form, where signing in
   * again would fail for the same reason and say nothing about it. Only the
   * server refusing the session — 401 or 403 — actually means not signed in.
   */
  sessionError: unknown;
  login(email: string, password: string): Promise<void>;
  logout(): Promise<void>;
  refresh(): Promise<void>;
}
const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [sessionError, setSessionError] = useState<unknown>(null);
  const refresh = async () => {
    try {
      setUser(await get<User>("/api/v1/me"));
      setSessionError(null);
    } catch (error) {
      const refused =
        error instanceof APIError &&
        (error.status === 401 || error.status === 403);
      if (refused) {
        // The server looked and said no. That is the only answer that means the
        // reader has to sign in.
        setUser(null);
        setSessionError(null);
      } else {
        // Anything else is not an answer about the session at all. Clearing the
        // user here would sign somebody out of a session the server never
        // objected to, and send them to a form that will fail the same way.
        setSessionError(error);
      }
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void refresh();
  }, []);
  const value = useMemo<AuthValue>(
    () => ({
      user,
      loading,
      sessionError,
      login: async (email, password) => {
        setUser(await post<User>("/api/v1/auth/login", { email, password }));
      },
      logout: async () => {
        // Cleared locally whatever the server says. Somebody who asked to leave
        // has left, and a failed request must not keep them looking signed in.
        try {
          await post("/api/v1/auth/logout");
        } finally {
          setUser(null);
          setSessionError(null);
        }
      },
      refresh,
    }),
    [user, loading, sessionError],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("AuthProvider missing");
  return value;
}
