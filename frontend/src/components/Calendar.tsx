import React, { useMemo } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';

interface Clip {
  ID: number;
  timestamp: string;
  event: string;
  start_time?: Date;
  date_key?: string;
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

  // Bolt Optimization: Memoize the set of days with clips to avoid O(N) iteration on every render.
  // Use pre-calculated date_key if available.
  const clipDays = useMemo(() => new Set(
    clips.map(c => c.date_key || new Date(c.timestamp).toDateString())
  ), [clips]);

  const handlePrevMonth = () => {
    setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() - 1, 1));
  };

  const handleNextMonth = () => {
    setViewDate(new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 1));
  };

  const handleJumpToToday = () => {
    const today = new Date();
    setViewDate(today);
    onDateSelect(today);
  };

  const isCurrentMonth = () => {
    const today = new Date();
    return (
      viewDate.getMonth() === today.getMonth() &&
      viewDate.getFullYear() === today.getFullYear()
    );
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
      days.push(<div key={`pad-${i}`} className="h-11 w-11" />);
    }

    for (let i = 1; i <= daysInMonth; i++) {
      const hasFootage = hasClips(i);
      const selected = isSelected(i);
      const date = new Date(viewDate.getFullYear(), viewDate.getMonth(), i);
      const dateStr = date.toLocaleDateString(undefined, {
        weekday: 'long',
        month: 'long',
        day: 'numeric',
      });
      const label = `${dateStr}${hasFootage ? ', has footage' : ''}${selected ? ', selected' : ''}`;

      days.push(
        <button
          key={i}
          onClick={() => onDateSelect(date)}
          aria-label={label}
          className={`
            h-11 w-11 rounded-[3px] flex items-center justify-center text-[16px] outline-none
            ${selected ? 'bg-[var(--accent)] text-[var(--bg)] font-bold' : ''}
            ${!selected && hasFootage ? 'bg-[var(--ghost)] text-[var(--ink)] font-medium' : ''}
            ${!selected && !hasFootage ? 'text-[var(--muted)] hover:bg-[var(--panel-2)]' : ''}
          `}
        >
          {i}
        </button>
      );
    }
    return days;
  };

  // Sync viewDate if currentDate changes drastically (optional, but good UX)
  // Currently logic only sets viewDate on init. If user changes date via external means, viewDate might be out of sync.
  // But for now, we keep existing logic to minimize regression risk.

  return (
    <div className="bg-[var(--panel-2)] rounded-[5px] p-4 border border-[var(--line)]">
      <div className="flex justify-between items-center mb-4">
        <button
          onClick={handlePrevMonth}
          className="desk-iconbtn"
          aria-label="Previous Month"
        >
          <ChevronLeft size={16} />
        </button>
        <span className="font-semibold text-[16px] text-[var(--ink)]">
          {viewDate.toLocaleString('default', { month: 'long', year: 'numeric' })}
        </span>
        <div className="flex items-center gap-1">
          {!isCurrentMonth() && (
            <button
              onClick={handleJumpToToday}
              className="desk-lightbtn px-2"
              aria-label="Jump to Today"
              title="Jump to Today"
            >
              Today
            </button>
          )}
          <button
            onClick={handleNextMonth}
            className="desk-iconbtn"
            aria-label="Next Month"
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-7 gap-1 text-center mb-2">
        {['S', 'M', 'T', 'W', 'T', 'F', 'S'].map((d, i) => (
          <div key={`${d}-${i}`} className="text-[16px] text-[var(--muted)] font-mono">{d}</div>
        ))}
      </div>

      <div className="grid grid-cols-7 gap-1 place-items-center">
        {renderDays()}
      </div>
    </div>
  );
};

// Bolt Optimization: Prevent re-renders when parent (Sidebar) updates irrelevant state
export default React.memo(Calendar);
