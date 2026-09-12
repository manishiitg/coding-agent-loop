import { useEffect, useState } from 'react';

interface PdfRendererProps {
  data: ArrayBuffer;
}

export function PdfRenderer({ data }: PdfRendererProps) {
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    const blob = new Blob([data], { type: 'application/pdf' });
    const objectUrl = URL.createObjectURL(blob);
    setUrl(objectUrl);

    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [data]);

  if (!url) {
    return <div className="p-4 text-gray-500">Loading PDF...</div>;
  }

  return (
    <div className="w-full h-full min-h-0">
      <iframe
        src={url}
        className="block h-full w-full border-0"
        title="PDF Viewer"
      />
    </div>
  );
}
