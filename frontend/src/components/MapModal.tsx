import React, { useMemo } from 'react';
import { MapContainer, TileLayer, Marker, Popup } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import 'leaflet/dist/leaflet.css';
import { X } from 'lucide-react';
import L from 'leaflet';

// Fix for default Leaflet icon not showing
import icon from 'leaflet/dist/images/marker-icon.png';
import iconShadow from 'leaflet/dist/images/marker-shadow.png';

let DefaultIcon = L.icon({
    iconUrl: icon,
    shadowUrl: iconShadow,
    iconSize: [25, 41],
    iconAnchor: [12, 41]
});

L.Marker.prototype.options.icon = DefaultIcon;


interface Clip {
    ID: number;
    timestamp: string;
    event: string;
    city: string;
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

const MapModal: React.FC<MapModalProps> = ({ isOpen, onClose, clips, onClipSelect }) => {
    // We filter clips that have valid coordinates
    const mapClips = useMemo(() => {
        return clips.filter(c =>
            c.telemetry &&
            typeof c.telemetry.latitude === 'number' &&
            typeof c.telemetry.longitude === 'number' &&
            c.telemetry.latitude !== 0 &&
            c.telemetry.longitude !== 0
        );
    }, [clips]);

    if (!isOpen) return null;

    // Calculate center based on clips, or default to 0,0
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
                        className="bg-white/90 text-black p-2 rounded-full shadow-lg hover:bg-white transition-colors"
                        aria-label="Close Map"
                    >
                        <X size={24} />
                    </button>
                </div>

                <div className="w-full h-full">
                     <MapContainer
                        center={center}
                        zoom={13}
                        scrollWheelZoom={true}
                        className="w-full h-full z-0"
                     >
                        <TileLayer
                            attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
                            url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
                        />
                        <MarkerClusterGroup chunkedLoading>
                            {mapClips.map((clip) => (
                                <Marker
                                    key={clip.ID}
                                    position={[clip.telemetry!.latitude, clip.telemetry!.longitude]}
                                >
                                    <Popup>
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
                                                       src={`/api/thumbnail${clip.video_files.find((v:any) => v.camera === 'Front').file_path}`}
                                                       alt="Thumbnail"
                                                       className="w-full h-full object-cover group-hover:scale-105 transition-transform"
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
