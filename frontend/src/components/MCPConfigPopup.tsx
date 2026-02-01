import React from 'react';
import { ArrowLeft } from 'lucide-react';
import MCPConfigEditor from './MCPConfigEditor';

interface MCPConfigPopupProps {
  onClose: () => void;
  onConfigChange?: () => void;
  initialView?: 'list' | 'json';
}

export const MCPConfigPopup: React.FC<MCPConfigPopupProps> = ({
  onClose,
  onConfigChange
}) => {
  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-6xl h-[90vh] overflow-y-auto relative">
        {/* Back button in the top-left */}
        <button
          onClick={onClose}
          className="absolute top-6 left-6 p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded-md hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors z-10 flex items-center gap-2"
          title="Back to Server Details"
        >
          <ArrowLeft className="w-5 h-5" />
          <span className="text-sm font-medium">Back</span>
        </button>

        <div className="mt-8">
          <MCPConfigEditor
            onConfigChange={onConfigChange}
            onClose={onClose}
          />
        </div>
      </div>
    </div>
  );
};

export default MCPConfigPopup;
