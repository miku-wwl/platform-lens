import { lifecycleStates, type RunState } from "../api/types";

export function Lifecycle({ state }: { state: RunState }) {
  return (
    <div className="lifecycle" aria-label={`Current lifecycle state: ${state}`}>
      {lifecycleStates.map((item, index) => (
        <div className="lifecycle-step" key={item}>
          <span className={`lifecycle-node ${item === state ? "current" : ""} ${state === "FAILED" && item !== state ? "not-current" : ""}`}>
            {item}
          </span>
          {index < lifecycleStates.length - 1 && <span className="lifecycle-arrow" aria-hidden="true">→</span>}
        </div>
      ))}
      <div className="lifecycle-step">
        <span className={`lifecycle-node terminal ${state === "FAILED" ? "current failed" : "not-current"}`}>FAILED</span>
      </div>
    </div>
  );
}
