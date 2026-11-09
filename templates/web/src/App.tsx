import { useState } from "react";
import { z } from "zod";
import { clearAll, load, save } from "./storage";

const Visits = z.number().int().nonnegative();

export function App() {
  const [visits] = useState(() => {
    const n = load("visits", Visits, 0) + 1;
    save("visits", n);
    return n;
  });

  return (
    <main>
      <h1>{"{{NAME}}"}</h1>
      <p data-testid="goal">{"{{GOAL}}"}</p>
      <p data-testid="visits">この端末で {visits} 回目</p>
      <footer>
        <p>データはこの端末の中にだけ保存されます。</p>
        <button
          type="button"
          onClick={() => {
            clearAll();
            location.reload();
          }}
        >
          データを消す
        </button>
      </footer>
    </main>
  );
}
