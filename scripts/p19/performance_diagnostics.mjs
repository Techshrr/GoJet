// Serialized into the browser before navigation; no module-scope dependencies.
// https://developer.chrome.com/docs/web-platform/long-animation-frames
export function installFrameDiagnostics() {
  const supported = PerformanceObserver.supportedEntryTypes.includes('long-animation-frame');
  const frames = [];
  const append = entries => {
    for (const entry of entries) frames.push({
      startTime: entry.startTime, duration: entry.duration,
      blockingDuration: entry.blockingDuration, firstUIEventTimestamp: entry.firstUIEventTimestamp,
      renderStart: entry.renderStart, styleAndLayoutStart: entry.styleAndLayoutStart,
      scripts: entry.scripts.map(script => ({
        startTime: script.startTime, duration: script.duration,
        executionStart: script.executionStart,
        forcedStyleAndLayoutDuration: script.forcedStyleAndLayoutDuration,
        pauseDuration: script.pauseDuration, invoker: script.invoker, invokerType: script.invokerType,
        sourceURL: script.sourceURL, sourceFunctionName: script.sourceFunctionName,
        sourceCharPosition: script.sourceCharPosition, windowAttribution: script.windowAttribution,
      })),
    });
  };
  let observer;
  if (supported) {
    observer = new PerformanceObserver(list => append(list.getEntries()));
    observer.observe({type: 'long-animation-frame', buffered: true});
  }
  window.__p19ReadFrameDiagnostics = () => {
    // Include entries queued before the final read but not yet delivered.
    if (observer) append(observer.takeRecords());
    return {supported, frames, scope: 'supplemental attribution; does not replace long-task or INP admission'};
  };
}
