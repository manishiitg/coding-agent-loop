import React, { useRef, useEffect, useCallback } from 'react'
import Editor from '@monaco-editor/react'
import type { OnChange, OnMount } from '@monaco-editor/react'
import type { editor } from 'monaco-editor'
import { useTheme } from '../../hooks/useTheme'

interface FileEditorProps {
  value: string
  filepath: string
  readOnly?: boolean
  onChange?: (value: string) => void
  height?: string
  onMount?: (editor: editor.IStandaloneCodeEditor) => void
}

export const FileEditor: React.FC<FileEditorProps> = ({
  value,
  filepath,
  readOnly = false,
  onChange,
  height = '100%',
  onMount
}) => {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null)
  const { theme } = useTheme()

  // Detect language from file extension
  const getLanguage = (path: string): string => {
    const ext = path.toLowerCase().split('.').pop() || ''
    const languageMap: Record<string, string> = {
      'json': 'json',
      'md': 'markdown',
      'markdown': 'markdown',
      'js': 'javascript',
      'jsx': 'javascript',
      'ts': 'typescript',
      'tsx': 'typescript',
      'py': 'python',
      'go': 'go',
      'java': 'java',
      'c': 'c',
      'cpp': 'cpp',
      'cs': 'csharp',
      'php': 'php',
      'rb': 'ruby',
      'sql': 'sql',
      'html': 'html',
      'css': 'css',
      'yaml': 'yaml',
      'yml': 'yaml',
      'xml': 'xml',
      'sh': 'shell',
      'bash': 'shell',
      'zsh': 'shell',
      'txt': 'plaintext'
    }
    return languageMap[ext] || 'plaintext'
  }

  // Map theme to Monaco theme
  const getMonacoTheme = (theme: string): string => {
    if (theme === 'dark' || theme === 'dark-plus') {
      return 'vs-dark'
    }
    return 'vs'
  }

  const handleEditorDidMount: OnMount = (editor) => {
    editorRef.current = editor

    // Configure editor options
    editor.updateOptions({
      fontSize: 14,
      minimap: { enabled: true },
      wordWrap: 'on',
      lineNumbers: 'on',
      folding: true,
      scrollBeyondLastLine: false,
      automaticLayout: true,
      formatOnPaste: true,
      formatOnType: false
    })

    if (onMount) {
      onMount(editor)
    }
  }

  const handleChange: OnChange = (value) => {
    if (onChange && value !== undefined) {
      onChange(value)
    }
  }

  // Format JSON helper
  const formatJSON = useCallback(() => {
    if (editorRef.current && getLanguage(filepath) === 'json') {
      try {
        const currentValue = editorRef.current.getValue()
        const parsed = JSON.parse(currentValue)
        const formatted = JSON.stringify(parsed, null, 2)
        editorRef.current.setValue(formatted)
        
        // Format the document
        editorRef.current.getAction('editor.action.formatDocument')?.run()
      } catch (error) {
        console.error('Failed to format JSON:', error)
      }
    }
  }, [filepath])

  // Expose format function via ref
  useEffect(() => {
    if (editorRef.current) {
      // Store format function on editor instance
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(editorRef.current as any).formatJSON = formatJSON
    }
  }, [formatJSON])

  return (
    <div className="h-full w-full">
      <Editor
        height={height}
        language="plaintext"
        value={value}
        theme={getMonacoTheme(theme)}
        onChange={handleChange}
        onMount={handleEditorDidMount}
        options={{
          readOnly,
          minimap: { enabled: true },
          wordWrap: 'on',
          lineNumbers: 'on',
          folding: true,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          formatOnPaste: true,
          formatOnType: false,
          fontSize: 14,
          fontFamily: 'Menlo, Monaco, "Courier New", monospace',
          tabSize: 2,
          insertSpaces: true,
          detectIndentation: false
        }}
      />
    </div>
  )
}

export default FileEditor

