import { useState } from 'react'

type Theme = 'light' | 'dark'

function currentTheme(): Theme {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme)

  function toggleTheme() {
    const nextTheme: Theme = theme === 'dark' ? 'light' : 'dark'
    const root = document.documentElement

    root.classList.add('theme-transition')
    root.classList.toggle('dark', nextTheme === 'dark')
    root.style.colorScheme = nextTheme
    try {
      localStorage.setItem('theme', nextTheme)
    } catch {
      // Тема всё равно переключится, даже если хранилище браузера недоступно.
    }
    setTheme(nextTheme)
    window.setTimeout(() => root.classList.remove('theme-transition'), 300)
  }

  const isDark = theme === 'dark'

  return (
    <button
      type="button"
      role="switch"
      aria-checked={isDark}
      aria-label={isDark ? 'Включить светлую тему' : 'Включить тёмную тему'}
      title={isDark ? 'Светлая тема' : 'Тёмная тема'}
      onClick={toggleTheme}
      className="relative h-7 w-12 shrink-0 cursor-pointer rounded-full bg-gray-200 shadow-inner transition-colors duration-300 ease-in-out focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:bg-gray-700"
    >
      <span className="absolute left-1 top-1 flex size-5 items-center justify-center rounded-full bg-white text-amber-500 shadow-sm transition-transform duration-300 ease-in-out dark:translate-x-5 dark:bg-gray-900 dark:text-blue-300">
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          className="absolute size-3.5 rotate-0 opacity-100 transition-all duration-300 ease-in-out dark:rotate-90 dark:opacity-0"
        >
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.66 6.34l1.41-1.41" />
        </svg>
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          className="absolute size-3.5 -rotate-90 opacity-0 transition-all duration-300 ease-in-out dark:rotate-0 dark:opacity-100"
        >
          <path d="M20.4 15.5A8.5 8.5 0 0 1 8.5 3.6 8.5 8.5 0 1 0 20.4 15.5Z" />
        </svg>
      </span>
    </button>
  )
}
