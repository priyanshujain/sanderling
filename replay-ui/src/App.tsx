import { BrowserRouter, Navigate, Route, Routes, useParams } from "react-router-dom";
import RunList from "./routes/RunList";
import RunDetail from "./routes/RunDetail";

function RunIndexRedirect() {
  const { id } = useParams<{ id: string }>();
  if (!id) {
    return <NotFound />;
  }
  return <Navigate to={`/runs/${id}/steps/1`} replace />;
}

function NotFound() {
  return <div className="status-block">not found</div>;
}

export default function App() {
  return (
    <BrowserRouter>
      <div className="app-shell">
        <main className="app-main">
          <Routes>
            <Route path="/" element={<RunList />} />
            <Route path="/runs/:id" element={<RunIndexRedirect />} />
            <Route path="/runs/:id/steps/:step" element={<RunDetail />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  );
}
