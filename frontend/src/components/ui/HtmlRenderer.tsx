import { useEffect, useRef, useState, type Ref, type SyntheticEvent } from 'react';

interface HtmlRendererProps {
  content: string;
  onLinkClick?: (href: string) => boolean;
  autoHeight?: boolean;
  initialHeight?: number;
  iframeRef?: Ref<HTMLIFrameElement>;
  onFrameLoad?: (frame: HTMLIFrameElement) => void;
}

export function HtmlRenderer({ content, onLinkClick, autoHeight = false, initialHeight = 600, iframeRef, onFrameLoad }: HtmlRendererProps) {
  const [frameHeight, setFrameHeight] = useState(initialHeight);
  const frameCleanupRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (autoHeight) setFrameHeight(initialHeight);
  }, [autoHeight, content, initialHeight]);

  useEffect(() => () => frameCleanupRef.current?.(), []);

  if (!content) {
    return <div className="p-4 text-gray-500">Loading HTML...</div>;
  }

  const handleLoad = (event: SyntheticEvent<HTMLIFrameElement>) => {
    frameCleanupRef.current?.();
    frameCleanupRef.current = null;

    const frameDocument = event.currentTarget.contentDocument;
    if (!frameDocument) return;

    const handleClick = (clickEvent: MouseEvent) => {
      if (!onLinkClick) return;
      const target = clickEvent.target as { closest?: (selector: string) => HTMLAnchorElement | null } | null;
      const anchor = target?.closest?.('a[href]');
      const href = anchor?.getAttribute('href');
      if (href && onLinkClick(href)) {
        clickEvent.preventDefault();
      }
    };
    frameDocument.addEventListener('click', handleClick);

    let resizeObserver: ResizeObserver | null = null;
    let animationFrame = 0;
    if (autoHeight) {
      frameDocument.documentElement.style.overflow = 'hidden';
      if (frameDocument.body) frameDocument.body.style.overflow = 'hidden';

      const measure = () => {
        window.cancelAnimationFrame(animationFrame);
        animationFrame = window.requestAnimationFrame(() => {
          const body = frameDocument.body;
          const root = frameDocument.documentElement;
          const height = Math.ceil(Math.max(
            root.scrollHeight,
            root.offsetHeight,
            body?.scrollHeight || 0,
            body?.offsetHeight || 0,
          ));
          if (height > 0) setFrameHeight(previous => Math.abs(previous - height) > 1 ? height : previous);
        });
      };

      resizeObserver = new ResizeObserver(measure);
      resizeObserver.observe(frameDocument.documentElement);
      if (frameDocument.body) resizeObserver.observe(frameDocument.body);
      measure();
    }

    frameCleanupRef.current = () => {
      frameDocument.removeEventListener('click', handleClick);
      resizeObserver?.disconnect();
      window.cancelAnimationFrame(animationFrame);
    };
    onFrameLoad?.(event.currentTarget);
  };

  return (
    <div className={autoHeight ? 'w-full' : 'w-full h-full flex flex-col'}>
      <iframe
        ref={iframeRef}
        srcDoc={content}
        className={autoHeight ? 'block w-full border-0' : 'flex-1 w-full border-0'}
        style={autoHeight ? { height: `${frameHeight}px` } : undefined}
        title="HTML Viewer"
        sandbox="allow-same-origin allow-scripts"
        onLoad={handleLoad}
      />
    </div>
  );
}
