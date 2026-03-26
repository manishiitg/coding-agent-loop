import { useState } from 'react'
import { ChevronDown, Sun, Moon } from 'lucide-react'
import { useTheme } from '../hooks/useTheme'

const themeOptions = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
]

type ThemeDropdownProps = {
  compact?: boolean
}

export default function ThemeDropdown({ compact = false }: ThemeDropdownProps) {
  const { theme, setTheme } = useTheme()
  const [isOpen, setIsOpen] = useState(false)

  const currentTheme = themeOptions.find(option => option.value === theme) || themeOptions[0]
  const CurrentIcon = currentTheme.icon

  const handleThemeChange = (newTheme: string) => {
    setTheme(newTheme as 'light' | 'dark')
    setIsOpen(false)
  }

  return (
    <div className="relative">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className={`flex items-center rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-sm ${
          compact ? 'gap-1 px-2 py-1.5' : 'gap-2 px-3 py-1.5'
        }`}
        title="Select theme"
      >
        <CurrentIcon className="w-4 h-4 text-gray-600 dark:text-gray-400" />
        {!compact && (
          <span className="text-gray-700 dark:text-gray-300">
            {currentTheme.label}
          </span>
        )}
        <ChevronDown className="w-3 h-3 text-gray-500 dark:text-gray-500" />
      </button>

      {isOpen && (
        <>
          {/* Backdrop */}
          <div 
            className="fixed inset-0 z-10" 
            onClick={() => setIsOpen(false)}
          />
          
          {/* Dropdown */}
          <div className="absolute right-0 top-full mt-1 w-32 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg z-20">
            {themeOptions.map((option) => {
              const Icon = option.icon
              const isSelected = option.value === theme
              
              return (
                <button
                  key={option.value}
                  onClick={() => handleThemeChange(option.value)}
                  className={`w-full flex items-center gap-2 px-3 py-2 text-sm text-left hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors first:rounded-t-md last:rounded-b-md ${
                    isSelected 
                      ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300' 
                      : 'text-gray-700 dark:text-gray-300'
                  }`}
                >
                  <Icon className="w-4 h-4" />
                  <span>{option.label}</span>
                  {isSelected && (
                    <div className="ml-auto w-2 h-2 bg-blue-600 dark:bg-blue-400 rounded-full" />
                  )}
                </button>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}
