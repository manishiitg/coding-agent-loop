import React, { useState } from 'react'
import type { PreValidationCompletedEvent, FileCheckResultForEvent, ValidationErrorForEvent } from '../../../generated/event-types'

type PreValidationCompletedEventData = PreValidationCompletedEvent

interface PreValidationCompletedEventDisplayProps {
  event: PreValidationCompletedEventData
  compact?: boolean
}

export const PreValidationCompletedEventDisplay: React.FC<PreValidationCompletedEventDisplayProps> = ({ 
  event, 
  compact = false 
}) => {
  const [isExpanded, setIsExpanded] = useState(false)

  const totalChecks = event.total_checks ?? 0
  const passedChecks = event.passed_checks ?? 0
  const overallPass = event.overall_pass ?? false
  const passRate = totalChecks > 0 
    ? Math.round((passedChecks / totalChecks) * 100) 
    : 0

  return (
    <div className={`${
      overallPass 
        ? 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800' 
        : 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800'
    } rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} ${
        overallPass 
          ? 'text-green-700 dark:text-green-300' 
          : 'text-red-700 dark:text-red-300'
      }`}>
        <div className="font-medium flex items-center gap-2">
          <span>🔍 Pre-Validation {overallPass ? 'Passed' : 'Failed'}</span>
          <span className={`px-2 py-0.5 rounded text-xs font-semibold ${
            overallPass 
              ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' 
              : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
          }`}>
            {passedChecks}/{totalChecks} checks passed ({passRate}%)
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
          overallPass 
            ? 'text-green-600 dark:text-green-400' 
            : 'text-red-600 dark:text-red-400'
        } mt-2`}>
          <div className="grid grid-cols-3 gap-2">
            <div>
              <div className="font-medium">Total Checks</div>
              <div>{totalChecks}</div>
            </div>
            <div>
              <div className="font-medium">Passed</div>
              <div className="text-green-600 dark:text-green-400">{passedChecks}</div>
            </div>
            <div>
              <div className="font-medium">Failed</div>
              <div className="text-red-600 dark:text-red-400">{event.failed_checks ?? 0}</div>
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
                {event.files_checked.map((fileCheck: FileCheckResultForEvent, idx: number) => (
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
              {event.errors.map((error: ValidationErrorForEvent, idx: number) => (
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

