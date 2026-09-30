import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { RecordDraftProvider } from './state/recordDraft';
import { AuthProvider } from './state/auth';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <RecordDraftProvider>
          <App />
        </RecordDraftProvider>
      </AuthProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
