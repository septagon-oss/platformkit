// Storybook owns controls and navigation; the authorized Go preview owns HTML,
// styles, controllers and prop validation. Its sandbox also applies to direct URLs.
export function render(args, context) {
  const { example, rawFields } = context.parameters.platformkit;
  const changes = Object.entries(args).filter(([key, value]) =>
    value !== undefined && JSON.stringify(value) !== JSON.stringify(context.initialArgs[key]));
  for (const [key, value] of changes) {
    if (!rawFields.includes(key)) continue;
    try { JSON.parse(value); } catch {
      const error = document.createElement('p');
      error.setAttribute('role', 'alert');
      error.textContent = `${key} must contain one valid JSON value.`;
      return error;
    }
  }
  const props = '{' + changes.map(([key, value]) =>
    JSON.stringify(key) + ':' + (rawFields.includes(key) ? value : JSON.stringify(value))).join(',') + '}';
  const url = new URL('../preview', window.location.href);
  url.searchParams.set('example', example);
  url.searchParams.set('props', props);
  url.searchParams.set('theme', context.globals.theme || 'light');
  const frame = document.createElement('iframe');
  frame.title = context.name;
  frame.setAttribute('sandbox', 'allow-scripts');
  frame.style.cssText = 'width:100%;height:75vh;min-height:360px;border:0';
  frame.src = url.href;
  return frame;
}

export default {
  parameters: { layout: 'fullscreen', controls: { expanded: true }, options: { storySort: { method: 'alphabetical' } } },
  initialGlobals: { theme: 'light', viewport: { value: 'desktop', isRotated: false } },
  globalTypes: {
    theme: {
      description: 'Go component theme',
      toolbar: { icon: 'paintbrush', dynamicTitle: true, items: ['light', 'dark', 'system'] },
    },
  },
};
