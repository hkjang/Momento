import "@fontsource-variable/inter";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import {
  MutationCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { CssBaseline, ThemeProvider } from "@mui/material";
import { AuthProvider } from "./contexts/AuthContext";
import { theme } from "./theme/theme";
import App from "./App";
import { ConfirmProvider } from "./components/ConfirmDialog";
import GlobalToast from "./components/GlobalToast";
import { showToast, successMessage } from "./components/toast";
import "./styles.css";

const queryClient = new QueryClient({
  // 조작이 meta.successMessage 를 선언하면 성공했을 때 알린다 (components/toast.ts).
  mutationCache: new MutationCache({
    onSuccess: (_data, _variables, _context, mutation) => {
      const message = successMessage(mutation.meta);
      if (message) showToast(message);
    },
  }),
  defaultOptions: {
    queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false },
  },
});
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <ConfirmProvider>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <AuthProvider>
              <App />
            </AuthProvider>
          </BrowserRouter>
        </QueryClientProvider>
        <GlobalToast />
      </ConfirmProvider>
    </ThemeProvider>
  </StrictMode>,
);
