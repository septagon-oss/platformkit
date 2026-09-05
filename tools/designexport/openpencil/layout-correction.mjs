// Layout owns temporary Yoga objects and grid sizing modes. Release/restore
// them at that boundary even when measurement or nested layout throws.
export function correctLayout(source, replace) {
  return replace(source,
    '\tyogaRoot.calculateLayout(void 0, void 0, yogaDirection);\n' +
    '\tapplyYogaLayout(graph, frame, yogaRoot, computeLayoutInternal);\n' +
    '\tfreeYogaTree(yogaRoot);',
    String.raw`
  try {
    yogaRoot.calculateLayout(void 0, void 0, yogaDirection);
    applyYogaLayout(graph, frame, yogaRoot, computeLayoutInternal);
  } finally {
    freeYogaTree(yogaRoot);
  }`)
}

export function correctGridRecompute(source, replace) {
  return replace(source,
    '\tcomputeLayout(graph, child.id);\n' +
    '\tconst restore = {};\n' +
    '\tif (updates.primaryAxisSizing) restore.primaryAxisSizing = savedPrimary;\n' +
    '\tif (updates.counterAxisSizing) restore.counterAxisSizing = savedCounter;\n' +
    '\tif (Object.keys(restore).length > 0) graph.updateNode(child.id, restore);',
    String.raw`
  const restore = {};
  if (updates.primaryAxisSizing) restore.primaryAxisSizing = savedPrimary;
  if (updates.counterAxisSizing) restore.counterAxisSizing = savedCounter;
  try {
    computeLayout(graph, child.id);
  } finally {
    if (Object.keys(restore).length > 0) graph.updateNode(child.id, restore);
  }`)
}
