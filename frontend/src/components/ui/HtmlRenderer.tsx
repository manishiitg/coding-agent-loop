import { useEffect, useState } from 'react';
import { useTheme } from '../../hooks/useTheme';

interface HtmlRendererProps {
  content: string;
}

// VS Code dark theme - matches index.css .dark variables
const darkModeStyles = `
<style data-theme="injected">
  *, *::before, *::after {
    border-color: hsl(0 0% 24%) !important; /* --border */
  }
  html, body {
    background-color: hsl(0 0% 12%) !important;  /* --background #1e1e1e */
    color: hsl(0 0% 83%) !important;              /* --foreground #d4d4d4 */
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    margin: 0; padding: 16px;
    color-scheme: dark;
  }
  a { color: hsl(200 100% 40%) !important; }                /* --primary #007acc */
  a:visited { color: hsl(280 60% 60%) !important; }
  h1, h2, h3, h4, h5, h6 {
    color: hsl(0 0% 83%) !important;              /* --foreground */
  }
  pre, code {
    background-color: hsl(0 0% 15%) !important;  /* --card #252526 */
    color: hsl(0 0% 83%) !important;              /* --foreground */
    border: 1px solid hsl(0 0% 24%) !important;   /* --border */
    border-radius: 4px;
  }
  pre { padding: 12px !important; overflow-x: auto; }
  code { padding: 2px 4px !important; }
  pre code { border: none !important; padding: 0 !important; }
  table {
    border-collapse: collapse !important;
    width: 100%;
  }
  th {
    background-color: hsl(0 0% 20%) !important;  /* --muted #333333 */
    color: hsl(0 0% 83%) !important;
  }
  td {
    background-color: hsl(0 0% 12%) !important;  /* --background */
    color: hsl(0 0% 83%) !important;
  }
  th, td {
    padding: 8px 12px !important;
    border: 1px solid hsl(0 0% 24%) !important;   /* --border */
  }
  tr:nth-child(even) td {
    background-color: hsl(0 0% 15%) !important;  /* --card */
  }
  blockquote {
    border-left: 3px solid hsl(200 100% 40%) !important; /* --primary */
    color: hsl(0 0% 60%) !important;              /* --muted-foreground */
    padding-left: 12px !important;
    margin-left: 0 !important;
  }
  hr {
    border-color: hsl(0 0% 24%) !important;       /* --border */
  }
  input, select, textarea, button {
    background-color: hsl(0 0% 20%) !important;  /* --input */
    color: hsl(0 0% 83%) !important;
    border: 1px solid hsl(0 0% 24%) !important;
  }
  img { opacity: 0.9; }
  mark {
    background-color: hsl(45 25% 25%) !important;
    color: hsl(0 0% 83%) !important;
  }
</style>
`;

// VS Code light theme - matches index.css :root variables
const lightModeStyles = `
<style data-theme="injected">
  *, *::before, *::after {
    border-color: hsl(0 0% 85%) !important; /* --border */
  }
  html, body {
    background-color: hsl(0 0% 100%) !important; /* --background #ffffff */
    color: hsl(0 0% 20%) !important;              /* --foreground #333333 */
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    margin: 0; padding: 16px;
    color-scheme: light;
  }
  a { color: hsl(200 100% 40%) !important; }                /* --primary #007acc */
  a:visited { color: hsl(280 60% 60%) !important; }
  h1, h2, h3, h4, h5, h6 {
    color: hsl(0 0% 20%) !important;              /* --foreground */
  }
  pre, code {
    background-color: hsl(0 0% 96%) !important;  /* --muted #f5f5f5 */
    color: hsl(0 0% 20%) !important;              /* --foreground */
    border: 1px solid hsl(0 0% 85%) !important;   /* --border */
    border-radius: 4px;
  }
  pre { padding: 12px !important; overflow-x: auto; }
  code { padding: 2px 4px !important; }
  pre code { border: none !important; padding: 0 !important; }
  table {
    border-collapse: collapse !important;
    width: 100%;
  }
  th {
    background-color: hsl(0 0% 96%) !important;  /* --muted */
    color: hsl(0 0% 20%) !important;
  }
  td {
    background-color: hsl(0 0% 100%) !important; /* --background */
    color: hsl(0 0% 20%) !important;
  }
  th, td {
    padding: 8px 12px !important;
    border: 1px solid hsl(0 0% 85%) !important;   /* --border */
  }
  tr:nth-child(even) td {
    background-color: hsl(0 0% 98%) !important;  /* --card */
  }
  blockquote {
    border-left: 3px solid hsl(200 100% 40%) !important; /* --primary */
    color: hsl(0 0% 45%) !important;              /* --muted-foreground */
    padding-left: 12px !important;
    margin-left: 0 !important;
  }
  hr {
    border-color: hsl(0 0% 85%) !important;       /* --border */
  }
  input, select, textarea, button {
    background-color: hsl(0 0% 96%) !important;  /* --input */
    color: hsl(0 0% 20%) !important;
    border: 1px solid hsl(0 0% 85%) !important;
  }
  mark {
    background-color: hsl(45 80% 80%) !important;
    color: hsl(0 0% 20%) !important;
  }
</style>
`;

function stripOriginalStyles(html: string): string {
  // Remove all <style> tags and their contents
  let cleaned = html.replace(/<style[\s\S]*?<\/style>/gi, '');
  // Remove all <link rel="stylesheet"> tags
  cleaned = cleaned.replace(/<link[^>]*rel=["']stylesheet["'][^>]*\/?>/gi, '');
  // Remove inline style attributes from all elements
  cleaned = cleaned.replace(/\s+style\s*=\s*(?:"[^"]*"|'[^']*')/gi, '');
  return cleaned;
}

function applyThemeStyles(html: string, theme: 'light' | 'dark'): string {
  const cleaned = stripOriginalStyles(html);
  const styles = theme === 'dark' ? darkModeStyles : lightModeStyles;

  // If there's a <head>, inject into it
  if (/<head[\s>]/i.test(cleaned)) {
    return cleaned.replace(/(<head[^>]*>)/i, `$1${styles}`);
  }
  // If there's an <html> tag, inject after it
  if (/<html[\s>]/i.test(cleaned)) {
    return cleaned.replace(/(<html[^>]*>)/i, `$1<head>${styles}</head>`);
  }
  // Otherwise prepend
  return styles + cleaned;
}

export function HtmlRenderer({ content }: HtmlRendererProps) {
  const { theme } = useTheme();
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    const themed = applyThemeStyles(content, theme);
    const blob = new Blob([themed], { type: 'text/html' });
    const objectUrl = URL.createObjectURL(blob);
    setUrl(objectUrl);

    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [content, theme]);

  if (!url) {
    return <div className="p-4 text-gray-500">Loading HTML...</div>;
  }

  return (
    <div className="w-full h-full flex flex-col">
      <iframe
        src={url}
        className="flex-1 w-full border-0"
        title="HTML Viewer"
        sandbox="allow-same-origin allow-scripts"
      />
    </div>
  );
}
