import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { CheckCircle, Info, XCircle } from 'lucide-react';

type ToastType = 'success' | 'info' | 'error';

interface ToastProps {
  message: string;
  type: ToastType;
  duration?: number;
  onClose: () => void;
}

const TOAST_CONFIG: Record<ToastType, { icon: typeof CheckCircle; bgColor: string }> = {
  success: { icon: CheckCircle, bgColor: 'bg-green-500' },
  info: { icon: Info, bgColor: 'bg-blue-500' },
  error: { icon: XCircle, bgColor: 'bg-red-500' },
};

export const Toast: React.FC<ToastProps> = ({
  message,
  type,
  duration = 3000,
  onClose
}) => {
  const [isVisible, setIsVisible] = useState(true);

  useEffect(() => {
    const timer = setTimeout(() => {
      setIsVisible(false);
      setTimeout(onClose, 300); // Allow fade out animation
    }, duration);

    return () => clearTimeout(timer);
  }, [duration, onClose]);

  if (!isVisible) return null;

  const { icon: Icon, bgColor } = TOAST_CONFIG[type];

  return (
    <div className="fixed top-4 right-4 z-[100000] animate-in slide-in-from-right-full duration-300">
      <div className={`${bgColor} text-white px-4 py-2 rounded-lg shadow-lg flex items-center gap-2 max-w-sm`}>
        <Icon className="w-4 h-4 flex-shrink-0" />
        <span className="text-sm font-medium">{message}</span>
      </div>
    </div>
  );
};

interface ToastContainerProps {
  toasts: Array<{ id: string; message: string; type: ToastType }>;
  onRemoveToast: (id: string) => void;
}

export const ToastContainer: React.FC<ToastContainerProps> = ({ toasts, onRemoveToast }) => {
  if (typeof document === 'undefined') return null;

  return createPortal(
    <>
      {toasts.map((toast) => (
        <Toast
          key={toast.id}
          message={toast.message}
          type={toast.type}
          onClose={() => onRemoveToast(toast.id)}
        />
      ))}
    </>,
    document.body
  );
};
