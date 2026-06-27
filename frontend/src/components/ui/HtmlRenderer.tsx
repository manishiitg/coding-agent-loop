interface HtmlRendererProps {
  content: string;
}

export function HtmlRenderer({ content }: HtmlRendererProps) {
  if (!content) {
    return <div className="p-4 text-gray-500">Loading HTML...</div>;
  }

  return (
    <div className="w-full h-full flex flex-col">
      <iframe
        srcDoc={content}
        className="flex-1 w-full border-0"
        title="HTML Viewer"
        sandbox="allow-same-origin allow-scripts"
      />
    </div>
  );
}
