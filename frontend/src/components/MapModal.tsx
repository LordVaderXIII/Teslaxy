import React, { useMemo, useEffect } from 'react';
import { MapContainer, TileLayer, Marker, Popup, useMap } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import 'leaflet/dist/leaflet.css';
import { X } from 'lucide-react';
import L from 'leaflet';

interface Clip {
    ID: number;
    timestamp: string;
    event: string;
    city: string;
    event_timestamp?: string;
    telemetry?: {
        latitude: number;
        longitude: number;
    };
    video_files?: any[];
}

interface MapModalProps {
    isOpen: boolean;
    onClose: () => void;
    clips: Clip[];
    onClipSelect: (clip: Clip) => void;
}

// Custom Icons
const createClusterCustomIcon = (cluster: any) => {
    return L.divIcon({
        html: `<div class="w-full h-full flex items-center justify-center bg-blue-600 text-white font-bold rounded-full border-2 border-gray-900 shadow-lg text-sm">${cluster.getChildCount()}</div>`,
        className: 'custom-cluster-icon', // Used for verification
        iconSize: L.point(40, 40, true),
    });
};

const customMarkerIcon = L.divIcon({
    html: `<div class="w-full h-full bg-blue-500 rounded-full border-2 border-white shadow-md"></div>`,
    className: 'custom-marker-icon', // Used for verification
    iconSize: L.point(16, 16, true), // Small dot
    iconAnchor: [8, 8], // Center it
});

// Component to auto-fit map bounds
const MapAutoFit = ({ clips }: { clips: Clip[] }) => {
    const map = useMap();

    useEffect(() => {
        if (clips.length === 0) return;

        const points = clips.map(c => L.latLng(c.telemetry!.latitude, c.telemetry!.longitude));
        const bounds = L.latLngBounds(points);

        if (bounds.isValid()) {
             map.fitBounds(bounds, { padding: [50, 50] });
        }
    }, [clips, map]);

    return null;
};

const MapModal: React.FC<MapModalProps> = ({ isOpen, onClose, clips, onClipSelect }) => {
    const mapClips = useMemo(() => {
        return clips.filter(c =>
            c.telemetry &&
            typeof c.telemetry.latitude === 'number' &&
            typeof c.telemetry.longitude === 'number' &&
            c.telemetry.latitude !== 0 &&
            c.telemetry.longitude !== 0
        );
    }, [clips]);

    const getThumbnailUrl = (clip: Clip) => {
        const frontVideo = clip.video_files?.find((v: any) => v.camera === 'Front');
        if (!frontVideo) return '';

        let url = `/api/thumbnail${frontVideo.file_path}`;
        if (clip.event_timestamp) {
            const start = new Date(clip.timestamp).getTime();
            const event = new Date(clip.event_timestamp).getTime();
            if (!isNaN(start) && !isNaN(event)) {
                 const diff = (event - start) / 1000;
                 if (diff > 0 && diff < 600) {
                     url += `?time=${diff.toFixed(1)}`;
                 }
            }
        }
        return url;
    };

    if (!isOpen) return null;

    const center: [number, number] = mapClips.length > 0
        ? [mapClips[0].telemetry!.latitude, mapClips[0].telemetry!.longitude]
        : [0, 0];

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4 animate-in fade-in duration-200">
            <div className="bg-gray-900 border border-gray-700 rounded-2xl w-full h-full max-w-6xl max-h-[90vh] flex flex-col shadow-2xl overflow-hidden relative">

                {/* Header */}
                <div className="absolute top-4 right-4 z-[1000]">
                    <button
                        onClick={onClose}
                        className="bg-gray-800 text-white p-2 rounded-full shadow-lg hover:bg-gray-700 transition-colors border border-gray-600"
                        aria-label="Close Map"
                    >
                        <X size={24} />
                    </button>
                </div>

                <div className="w-full h-full bg-[#1e1e1e]">
                     <MapContainer
                        center={center}
                        zoom={13}
                        scrollWheelZoom={true}
                        className="w-full h-full z-0"
                     >
                        <TileLayer
                            attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>'
                            url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
                        />
                        <MapAutoFit clips={mapClips} />
                        <MarkerClusterGroup
                            chunkedLoading
                            iconCreateFunction={createClusterCustomIcon}
                            spiderfyOnMaxZoom={true}
                            showCoverageOnHover={false}
                        >
                            {mapClips.map((clip) => (
                                <Marker
                                    key={clip.ID}
                                    position={[clip.telemetry!.latitude, clip.telemetry!.longitude]}
                                    icon={customMarkerIcon}
                                >
                                    <Popup className="custom-popup">
                                        <div className="w-48 text-gray-900">
                                            <div className="font-bold mb-1">{clip.city || 'Unknown Location'}</div>
                                            <div className="text-xs text-gray-600 mb-2">
                                                {new Date(clip.timestamp).toLocaleString()}
                                            </div>

                                            <button
                                                onClick={() => {
                                                    onClipSelect(clip);
                                                    onClose();
                                                }}
                                                className="w-full group relative aspect-video bg-gray-200 rounded overflow-hidden"
                                            >
                                               {clip.video_files?.find((v:any) => v.camera === 'Front') ? (
                                                   <img
                                                       src={getThumbnailUrl(clip)}
                                                       alt="Thumbnail"
                                                       className="w-full h-full object-cover group-hover:scale-105 transition-transform"
                                                       onError={(e) => {
                                                           e.currentTarget.style.display = 'none';
                                                           e.currentTarget.parentElement?.classList.add('flex', 'items-center', 'justify-center', 'bg-gray-800', 'text-gray-400');
                                                           if (e.currentTarget.parentElement) {
                                                               e.currentTarget.parentElement.innerText = clip.event;
                                                           }
                                                       }}
                                                   />
                                               ) : (
                                                   <div className="flex items-center justify-center h-full text-xs font-bold uppercase text-gray-500">
                                                       {clip.event}
                                                   </div>
                                               )}
                                               <div className="absolute inset-0 flex items-center justify-center bg-black/20 group-hover:bg-black/0 transition-colors">
                                                    <div className="bg-white/90 px-2 py-1 rounded text-xs font-bold shadow-sm">Play</div>
                                               </div>
                                            </button>
                                        </div>
                                    </Popup>
                                </Marker>
                            ))}
                        </MarkerClusterGroup>
                    </MapContainer>
                </div>
            </div>
        </div>
    );
};

export default MapModal;
