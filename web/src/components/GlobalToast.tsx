import { useEffect, useState } from "react";
import { Alert, Snackbar } from "@mui/material";
import { subscribeToasts, type Toast } from "./toast";

export default function GlobalToast() {
  const [toast, setToast] = useState<Toast | null>(null);
  useEffect(() => subscribeToasts(setToast), []);
  return (
    <Snackbar
      key={toast?.id}
      open={!!toast}
      autoHideDuration={3000}
      onClose={(_, reason) => {
        if (reason !== "clickaway") setToast(null);
      }}
      anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
    >
      <Alert
        severity="success"
        variant="filled"
        onClose={() => setToast(null)}
        sx={{ width: "100%" }}
      >
        {toast?.message}
      </Alert>
    </Snackbar>
  );
}
