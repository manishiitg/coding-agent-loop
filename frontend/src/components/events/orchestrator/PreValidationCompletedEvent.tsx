import React, { useState } from 'react'

interface FileCheckResultForEvent {
  file_name: string
  exists: boolean
  is_json: boolean
  json_checks: JSONCheckResultForEvent[]
}

interface JSONCheckResultForEvent {
  path: string
  passed: boolean
  check_type: string
  error_msg?: string
}

interface ValidationErrorForEvent {
  file: string
  path: string
  check_type: string
  expected: string
  actual: string
  message: string
}

interface PreValidationCompletedEventData {
  step_id?: string
  step_index?: number
  step_title?: string
  step_path?: string
  is_branch_step?: boolean
  overall_pass: boolean
  total_checks: number
  passed_checks: number
  failed_checks: number
  files_checked: FileCheckResultForEvent[]
  errors?: ValidationErrorForEvent[]
  run_folder?: string
  workspace_path?: string
}

interface PreValidationCompletedEventDisplayProps {
  event: PreValidationCompletedEventData
  compact?: boolean
}

export const PreValidationCompletedEventDisplay: React.FC<PreValidationCompletedEventDisplayProps> = ({ 
  event, 
  compact = false 
}) => {
  const [isExpanded, setIsExpanded] = useState(false)

  const passRate = event.total_checks > 0 
    ? Math.round((event.passed_checks / event.total_checks) * 100) 
    : 0

  return (
    <div className={`${
      event.overall_pass 
        ? 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800' 
        : 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800'
    } rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} ${
        event.overall_pass 
          ? 'text-green-700 dark:text-green-300' 
          : 'text-red-700 dark:text-red-300'
      }`}>
        <div className="font-medium flex items-center gap-2">
          <span>🔍 Pre-Validation {event.overall_pass ? 'Passed' : 'Failed'}</span>
          <span className={`px-2 py-0.5 rounded text-xs font-semibold ${
            event.overall_pass 
              ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' 
              : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
          }`}>
            {event.passed_checks}/{event.total_checks} checks passed ({passRate}%)
          </span>
        </div>
        
        {event.step_title && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${
            event.overall_pass 
              ? 'text-green-600 dark:text-green-400' 
              : 'text-red-600 dark:text-red-400'
          } mt-1`}>
            Step: {event.step_title}
          </div>
        )}
        
        {event.step_id && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${
            event.overall_pass 
              ? 'text-green-500 dark:text-green-500' 
              : 'text-red-500 dark:text-red-500'
          } mt-1`}>
            ID: {event.step_id}
          </div>
        )}

        {/* Summary Stats */}
        <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${
          event.overall_pass 
            ? 'text-green-600 dark:text-green-400' 
            : 'text-red-600 dark:text-red-400'
        } mt-2`}>
          <div className="grid grid-cols-3 gap-2">
            <div>
              <div className="font-medium">Total Checks</div>
              <div>{event.total_checks}</div>
            </div>
            <div>
              <div className="font-medium">Passed</div>
              <div className="text-green-600 dark:text-green-400">{event.passed_checks}</div>
            </div>
            <div>
              <div className="font-medium">Failed</div>
              <div className="text-red-600 dark:text-red-400">{event.failed_checks}</div>
            </div>
          </div>
        </div>

        {/* Files Checked */}
        {event.files_checked && event.files_checked.length > 0 && (
          <div className="mt-2">
            <button
              onClick={() => setIsExpanded(!isExpanded)}
              className={`${compact ? 'text-[10px]' : 'text-xs'} ${
                event.overall_pass 
                  ? 'text-green-600 dark:text-green-400 hover:text-green-700 dark:hover:text-green-300' 
                  : 'text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300'
              } font-medium underline`}
            >
              {isExpanded ? '▼' : '▶'} {event.files_checked.length} file(s) checked
            </button>
            
            {isExpanded && (
              <div className="mt-2 space-y-2">
                {event.files_checked.map((fileCheck, idx) => (
                  <div 
                    key={idx}
                    className={`${compact ? 'text-[10px]' : 'text-xs'} bg-white dark:bg-gray-800 rounded p-2 border ${
                      fileCheck.exists 
                        ? 'border-green-200 dark:border-green-800' 
                        : 'border-red-200 dark:border-red-800'
                    }`}
                  >
                    <div className="font-medium flex items-center gap-2">
                      <span>{fileCheck.file_name}</span>
                      <span className={`px-1.5 py-0.5 rounded text-[10px] ${
                        fileCheck.exists 
                          ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' 
                          : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
                      }`}>
                        {fileCheck.exists ? 'EXISTS' : 'MISSING'}
                      </span>
                      {fileCheck.is_json && (
                        <span className="px-1.5 py-0.5 rounded text-[10px] bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300">
                          JSON
                        </span>
                      )}
                    </div>
                    
                    {fileCheck.json_checks && fileCheck.json_checks.length > 0 && (
                      <div className="mt-1 space-y-1">
                        {fileCheck.json_checks.map((check, checkIdx) => (
                          <div 
                            key={checkIdx}
                            className={`flex items-center gap-2 ${
                              check.passed 
                                ? 'text-green-600 dark:text-green-400' 
                                : 'text-red-600 dark:text-red-400'
                            }`}
                          >
                            <span>{check.passed ? '✅' : '❌'}</span>
                            <span className="font-mono">{check.path}</span>
                            <span className="text-gray-500 dark:text-gray-400">({check.check_type})</span>
                            {check.error_msg && (
                              <span className="text-red-600 dark:text-red-400">- {check.error_msg}</span>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Errors */}
        {event.errors && event.errors.length > 0 && (
          <div className="mt-2">
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} font-medium text-red-600 dark:text-red-400`}>
              Validation Errors:
            </div>
            <div className="mt-1 space-y-1">
              {event.errors.map((error, idx) => (
                <div 
                  key={idx}
                  className={`${compact ? 'text-[10px]' : 'text-xs'} bg-red-50 dark:bg-red-900/20 rounded p-2 border border-red-200 dark:border-red-800`}
                >
                  <div className="font-medium">{error.check_type}</div>
                  {error.file && (
                    <div className="text-gray-600 dark:text-gray-400">File: {error.file}</div>
                  )}
                  {error.path && (
                    <div className="text-gray-600 dark:text-gray-400">Path: {error.path}</div>
                  )}
                  <div className="mt-1">
                    <div>Expected: {error.expected}</div>
                    <div>Actual: {error.actual}</div>
                  </div>
                  {error.message && (
                    <div className="mt-1 text-red-700 dark:text-red-300">{error.message}</div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

