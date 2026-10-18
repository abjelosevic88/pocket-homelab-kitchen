import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import Home from './pages/Home'
import Recipes from './pages/Recipes'
import RecipePage from './pages/Recipe'
import RecipeEdit from './pages/RecipeEdit'
import Plan from './pages/Plan'
import Shopping from './pages/Shopping'
import Pantry from './pages/Pantry'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="login-wrap"><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route element={<Gate><Layout /></Gate>}>
        <Route path="/" element={<Home />} />
        <Route path="/recipes" element={<Recipes />} />
        <Route path="/recipes/new" element={<RecipeEdit />} />
        <Route path="/recipes/:id" element={<RecipePage />} />
        <Route path="/recipes/:id/edit" element={<RecipeEdit />} />
        <Route path="/plan" element={<Plan />} />
        <Route path="/shopping" element={<Shopping />} />
        <Route path="/shopping/:id" element={<Shopping />} />
        <Route path="/pantry" element={<Pantry />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ToastProvider>
        <AppProvider>
          <App />
        </AppProvider>
      </ToastProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
