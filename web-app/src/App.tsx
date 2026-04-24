import { BrowserRouter, NavLink, Route, Routes, Navigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import QueuePage from './pages/QueuePage'
import SettingsPage from './pages/SettingsPage'
import LogsPage from './pages/LogsPage'
import './i18n'

const LANGS = [
  { code: 'zh-CN', label: '中文' },
  { code: 'en-US', label: 'EN' },
]

function NavBar() {
  const { t, i18n } = useTranslation()

  const switchLang = (code: string) => {
    i18n.changeLanguage(code)
    localStorage.setItem('language', code)
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
      isActive
        ? 'bg-blue-600 text-white'
        : 'text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
    }`

  return (
    <nav className="sticky top-0 z-10 bg-white/90 dark:bg-gray-900/90 backdrop-blur border-b border-gray-200 dark:border-gray-700">
      <div className="max-w-5xl mx-auto px-4 h-14 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="font-bold text-gray-900 dark:text-white mr-4">
            {t('app.title')}
          </span>
          <NavLink to="/queue" className={linkClass}>
            {t('nav.queue')}
          </NavLink>
          <NavLink to="/settings" className={linkClass}>
            {t('nav.settings')}
          </NavLink>
          <NavLink to="/logs" className={linkClass}>
            {t('nav.logs')}
          </NavLink>
        </div>
        <div className="flex items-center gap-1">
          {LANGS.map((lang) => (
            <button
              key={lang.code}
              onClick={() => switchLang(lang.code)}
              className={`px-3 py-1 rounded text-xs font-medium transition-colors ${
                i18n.language === lang.code
                  ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-white'
                  : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'
              }`}
            >
              {lang.label}
            </button>
          ))}
        </div>
      </div>
    </nav>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
        <NavBar />
        <main>
          <Routes>
            <Route path="/" element={<Navigate to="/queue" replace />} />
            <Route path="/queue" element={<QueuePage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/logs" element={<LogsPage />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}
