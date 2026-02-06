import React, { useState, useEffect, useRef } from 'react';
import { X, Download, AlertCircle, Loader2, CheckCircle } from 'lucide-react';
import type { Clip } from '../utils/clipMerge';

interface ExportModalProps {
    isOpen: boolean;
    onClose: () => void;
    clip: Clip;
    startTime: number;
    endTime: number;
    activeCamera: string;
}

interface ExportStatus {
    job_id: string;
    status: 'pending' | 'processing' | 'completed' | 'failed';
    progress: number;
    file_path: string;
    error?: string;
}

const ExportModal: React.FC<ExportModalProps> = ({
    isOpen,
    onClose,
    clip,
    startTime,
    endTime,
    activeCamera
}) => {
    const [status, setStatus] = useState<'idle' | 'submitting' | 'queued' | 'processing' | 'completed' | 'failed'>('idle');
    const [jobId, setJobId] = useState<string | null>(null);
    const [progress, setProgress] = useState(0);
    const [error, setError] = useState<string | null>(null);
    const [downloadPath, setDownloadPath] = useState<string | null>(null);
    const pollInterval = useRef<NodeJS.Timeout | null>(null);

    // Reset state when opening
    useEffect(() => {
        if (isOpen) {
            setStatus('idle');
            setJobId(null);
            setProgress(0);
            setError(null);
            setDownloadPath(null);
        } else {
            if (pollInterval.current) {
                clearInterval(pollInterval.current);
            }
        }
    }, [isOpen]);

    // Cleanup on unmount
    useEffect(() => {
        return () => {
            if (pollInterval.current) clearInterval(pollInterval.current);
        };
    }, []);

    const startExport = async () => {
        setStatus('submitting');
        setError(null);

        const duration = endTime - startTime;
        // Normalize camera name for API (e.g., "Left Repeater" -> "left_repeater" or however backend expects it)
        // Based on exporter.go, validCameras map includes "Front", "front", "Left Repeater", etc.
        // So passing "activeCamera" directly should be fine if it matches one of those keys.
        // Player.tsx uses "Front", "Left Repeater", etc.

        try {
            const res = await fetch('/api/export', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    clip_id: clip.ID,
                    cameras: [activeCamera],
                    start_time: startTime,
                    duration: duration
                })
            });

            if (!res.ok) {
                const data = await res.json();
                throw new Error(data.error || 'Failed to start export');
            }

            const data = await res.json();
            setJobId(data.job_id);
            setStatus('queued');

            // Start polling
            pollInterval.current = setInterval(() => checkStatus(data.job_id), 1000);

        } catch (err: any) {
            setStatus('failed');
            setError(err.message);
        }
    };

    const checkStatus = async (id: string) => {
        try {
            const res = await fetch(`/api/export/${id}`);
            if (!res.ok) throw new Error('Failed to fetch status');

            const data: ExportStatus = await res.json();

            if (data.status === 'processing') {
                setStatus('processing');
                setProgress(data.progress);
            } else if (data.status === 'completed') {
                setStatus('completed');
                setProgress(100);
                setDownloadPath(data.file_path);
                if (pollInterval.current) clearInterval(pollInterval.current);
            } else if (data.status === 'failed') {
                setStatus('failed');
                setError(data.error || 'Export failed');
                if (pollInterval.current) clearInterval(pollInterval.current);
            }
        } catch (err) {
            console.error(err);
            // Don't fail immediately on network blip, but maybe track errors?
        }
    };

    const handleClose = () => {
        if (pollInterval.current) clearInterval(pollInterval.current);
        onClose();
    };

    if (!isOpen) return null;

    const duration = (endTime - startTime).toFixed(1);

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4">
            <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl max-w-md w-full overflow-hidden">
                <div className="flex items-center justify-between p-4 border-b border-gray-800 bg-gray-800/50">
                    <h3 className="text-lg font-semibold text-white flex items-center gap-2">
                        <Download size={20} className="text-blue-400" />
                        Export Clip
                    </h3>
                    <button onClick={handleClose} className="text-gray-400 hover:text-white transition">
                        <X size={20} />
                    </button>
                </div>

                <div className="p-6 space-y-6">
                    {/* Summary */}
                    <div className="bg-black/30 rounded-lg p-4 space-y-2 text-sm border border-gray-800">
                        <div className="flex justify-between">
                            <span className="text-gray-400">Source Camera</span>
                            <span className="text-blue-300 font-medium">{activeCamera}</span>
                        </div>
                        <div className="flex justify-between">
                            <span className="text-gray-400">Start Time</span>
                            <span className="text-gray-200 font-mono">{startTime.toFixed(1)}s</span>
                        </div>
                         <div className="flex justify-between">
                            <span className="text-gray-400">End Time</span>
                            <span className="text-gray-200 font-mono">{endTime.toFixed(1)}s</span>
                        </div>
                        <div className="flex justify-between pt-2 border-t border-gray-800">
                            <span className="text-gray-400">Duration</span>
                            <span className="text-green-400 font-bold font-mono">{duration}s</span>
                        </div>
                    </div>

                    {/* Actions / Status */}
                    {status === 'idle' && (
                        <button
                            onClick={startExport}
                            className="w-full py-3 bg-blue-600 hover:bg-blue-500 text-white font-semibold rounded-lg transition shadow-lg shadow-blue-900/20 flex items-center justify-center gap-2"
                        >
                            <Download size={18} />
                            Start Export
                        </button>
                    )}

                    {(status === 'submitting' || status === 'queued' || status === 'processing') && (
                        <div className="space-y-2">
                            <div className="flex justify-between text-xs text-gray-400 uppercase font-bold tracking-wider">
                                <span>{status === 'queued' ? 'Queued...' : status === 'submitting' ? 'Submitting...' : 'Processing...'}</span>
                                <span>{progress.toFixed(0)}%</span>
                            </div>
                            <div className="h-2 bg-gray-800 rounded-full overflow-hidden">
                                <div
                                    className={`h-full bg-blue-500 transition-all duration-300 ${status === 'queued' ? 'animate-pulse' : ''}`}
                                    style={{ width: `${Math.max(5, progress)}%` }}
                                />
                            </div>
                            {status === 'processing' && (
                                <p className="text-xs text-center text-gray-500 animate-pulse">Encoding video...</p>
                            )}
                        </div>
                    )}

                    {status === 'completed' && downloadPath && (
                        <div className="space-y-4">
                            <div className="flex items-center gap-3 text-green-400 bg-green-900/20 p-3 rounded-lg border border-green-900/50">
                                <CheckCircle size={24} />
                                <div>
                                    <p className="font-medium">Export Complete!</p>
                                    <p className="text-xs text-green-300/70">Your video is ready to download.</p>
                                </div>
                            </div>
                            <a
                                href={`/api/downloads/${downloadPath}`}
                                download
                                className="block w-full py-3 bg-green-600 hover:bg-green-500 text-white font-semibold rounded-lg transition text-center shadow-lg shadow-green-900/20"
                            >
                                Download MP4
                            </a>
                        </div>
                    )}

                    {status === 'failed' && (
                        <div className="space-y-4">
                            <div className="flex items-center gap-3 text-red-400 bg-red-900/20 p-3 rounded-lg border border-red-900/50">
                                <AlertCircle size={24} />
                                <div>
                                    <p className="font-medium">Export Failed</p>
                                    <p className="text-xs text-red-300/70">{error || 'An unknown error occurred.'}</p>
                                </div>
                            </div>
                            <button
                                onClick={startExport}
                                className="w-full py-2 bg-gray-700 hover:bg-gray-600 text-white font-medium rounded-lg transition"
                            >
                                Try Again
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
};

export default ExportModal;
