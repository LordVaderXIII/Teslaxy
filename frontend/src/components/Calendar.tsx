import React from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';

interface Clip {
  ID: number;
  timestamp: string;
  event: string;
}

interface CalendarProps {
  currentDate: Date;
  onDateSelect: (date: Date) => void;
  clips: Clip[];
}

const Calendar: React.FC<CalendarProps> = ({ currentDate, onDateSelect, clips }) => {
  const [viewDate, setViewDate] = React.useState(currentDate);

  // Helper to get days in month
  const getDaysInMonth = (year: number, month: number) => {
    return new Date(year, month + 1, 0).getDate();
  };

  const getFirstDayOfMonth = (year: number, month: number) => {
    return new Date(year, month, 1).getDay();
  };

  const daysInMonth = getDaysInMonth(viewDate.getFullYear(), viewDate.getMonth());
  const firstDay = getFirstDayOfMonth(viewDate.getFullYear(), viewDate.getMonth());

  // Identify days with clips
  const clipDays = new Set(
    clips.map(c => new Date(c.timestamp).toDateString())
  );

  const handlePrevMonth = () => {
    setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() - 1, 1));
  };

  const handleNextMonth = () => {
    setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 1));
  };

  const isSelected = (day: number) => {
    const d = new Date(viewDate.getFullYear(), viewDate.getMonth(), day);
    return d.toDateString() === currentDate.toDateString();
  };

  const hasClips = (day: number) => {
    const d = new Date(viewDate.getFullYear(), viewDate.getMonth(), day);
    return clipDays.has(d.toDateString());
  };

  const renderDays = () => {
    const days = [];
    // Padding for first week
    for (let i = 0; i < firstDay; i++) {
      days.push(<div key={`pad-${i}`} className="h-8 w-8" />);
    }

    for (let i = 1; i <= daysInMonth; i++) {
      const hasFootage = hasClips(i);
      const selected = isSelected(i);

      days.push(
        <button
          key={i}
          onClick={() => onDateSelect(new Date(viewDate.getFullYear(), viewDate.getMonth(), i))}
          className={`
            h-8 w-8 rounded-full flex items-center justify-center text-sm transition
            ${selected ? 'bg-blue-600 text-white font-bold' : ''}
            ${!selected && hasFootage ? 'bg-gray-700 text-gray-200 font-medium' : ''}
            ${!selected && !hasFootage ? 'text-gray-500 hover:bg-gray-800' : ''}
          `}
        >
          {i}
        </button>
      );
    }
    return days;
  };

  return (
    <div className="bg-gray-900 rounded-lg p-4 border border-gray-800">
      <div className="flex justify-between items-center mb-4">
        <button onClick={handlePrevMonth} className="p-1 hover:bg-gray-800 rounded">
          <ChevronLeft size={16} />
        </button>
        <span className="font-semibold text-sm">
          {viewDate.toLocaleString('default', { month: 'long', year: 'numeric' })}
        </span>
        <button onClick={handleNextMonth} className="p-1 hover:bg-gray-800 rounded">
          <ChevronRight size={16} />
        </button>
      </div>

      <div className="grid grid-cols-7 gap-1 text-center mb-2">
        {['S', 'M', 'T', 'W', 'T', 'F', 'S'].map(d => (
          <div key={d} className="text-xs text-gray-500 font-mono">{d}</div>
        ))}
      </div>

      <div className="grid grid-cols-7 gap-1 place-items-center">
        {renderDays()}
      </div>
    </div>
  );
};

export default Calendar;
