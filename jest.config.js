// Delegate to the scaffolded jest config in .config/, which uses @swc/jest,
// allows ESM-only deps (d3, rxjs, etc.) through, and mocks react-inlinesvg.
module.exports = {
  ...require('./.config/jest.config'),
};
