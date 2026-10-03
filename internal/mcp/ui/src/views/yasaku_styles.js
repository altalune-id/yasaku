const yasakuStyles = css`
  * { box-sizing: border-box; }
  .app-swatch { width: 10px; height: 10px; border-radius: 2px; flex: none; }
  .app-bar {
    height: 6px;
    border-radius: 3px;
    flex: 1;
    background: var(--color-background-tertiary, light-dark(#e4e4e7, #2c2c30));
  }
  .app-bar > span { display: block; height: 100%; border-radius: 3px; }
  .app-chart { display: block; margin: 8px auto; }
  .app-buttons { display: flex; gap: 8px; }
  select, input {
    font: inherit;
    color: inherit;
    border-radius: 8px;
    padding: 4px 8px;
    border: 1px solid var(--color-border-primary, light-dark(#d4d4d8, #3f3f46));
    background: var(--color-background-primary, light-dark(#fff, #18181b));
  }
`;
