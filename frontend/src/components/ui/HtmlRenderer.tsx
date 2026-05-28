import { useEffect, useState } from 'react';

interface HtmlRendererProps {
  content: string;
}

export function HtmlRenderer({ content }: HtmlRendererProps) {
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    const blob = new Blob([content], { type: 'text/html' });
    const objectUrl = URL.createObjectURL(blob);
    setUrl(objectUrl);

    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [content]);

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
