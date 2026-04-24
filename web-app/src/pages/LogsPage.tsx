import { useTranslation } from 'react-i18next'

export default function LogsPage() {
  const { t } = useTranslation()

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-white mb-6">
        {t('logs.title')}
      </h1>
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-8 text-center">
        <p className="text-gray-400 dark:text-gray-500">
          {t('logs.noLogs')}
        </p>
        <p className="text-sm text-gray-400 dark:text-gray-500 mt-2">
          Logs are printed to the server console.
        </p>
      </div>
    </div>
  )
}
