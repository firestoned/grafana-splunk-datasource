// Mirrors .config/jest-setup.js. @swc/jest (configured by the scaffolding's
// jest.config.js) transforms this file, so `import` syntax is fine here.
import '@testing-library/jest-dom';

// jsdom doesn't expose TextEncoder/TextDecoder; React 18's server bundle
// (pulled in by @grafana/ui) needs them at module-eval time.
import { TextEncoder, TextDecoder } from 'util';
global.TextEncoder = TextEncoder;
global.TextDecoder = TextDecoder;

// jsdom doesn't implement matchMedia; @grafana/ui touches it in some paths.
Object.defineProperty(global, 'matchMedia', {
  writable: true,
  value: jest.fn().mockImplementation((query) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: jest.fn(), // deprecated
    removeListener: jest.fn(), // deprecated
    addEventListener: jest.fn(),
    removeEventListener: jest.fn(),
    dispatchEvent: jest.fn(),
  })),
});

HTMLCanvasElement.prototype.getContext = () => {};
