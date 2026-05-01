import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';

// Monaco doesn't render in jsdom — replace CodeEditor with a textarea so we
// can drive it like any other input. Keep all other @grafana/ui exports
// intact for the rest of the form.
jest.mock('@grafana/ui', () => {
  const actual = jest.requireActual('@grafana/ui');
  return {
    ...actual,
    CodeEditor: ({ value, onBlur, onSave }: any) => (
      <textarea
        data-testid="code-editor"
        defaultValue={value}
        onBlur={(e) => onBlur && onBlur((e.target as HTMLTextAreaElement).value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === 's') {
            onSave && onSave((e.currentTarget as HTMLTextAreaElement).value);
          }
        }}
      />
    ),
  };
});

import { QueryEditor } from './QueryEditor';
import { SplunkQuery } from '../types';

function setup(query: Partial<SplunkQuery> = {}) {
  const onChange = jest.fn();
  const onRunQuery = jest.fn();
  const fullQuery: SplunkQuery = {
    refId: 'A',
    search: '',
    ...query,
  };
  render(
    <QueryEditor
      query={fullQuery}
      onChange={onChange}
      onRunQuery={onRunQuery}
      datasource={{} as any}
    />
  );
  return { onChange, onRunQuery };
}

describe('QueryEditor', () => {
  it('renders SPL editor + numeric inputs + earliest/latest', () => {
    setup();
    expect(screen.getByTestId('code-editor')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('1000')).toBeInTheDocument();
    // Two "(panel range)" placeholders for earliest/latest
    expect(screen.getAllByPlaceholderText('(panel range)').length).toBe(2);
  });

  it('shows existing query values', () => {
    setup({
      search: 'index=main error',
      maxResults: 500,
      earliestTime: '-15m',
      latestTime: 'now',
    });
    expect((screen.getByTestId('code-editor') as HTMLTextAreaElement).value).toBe('index=main error');
    expect(screen.getByDisplayValue('500')).toBeInTheDocument();
    expect(screen.getByDisplayValue('-15m')).toBeInTheDocument();
    expect(screen.getByDisplayValue('now')).toBeInTheDocument();
  });

  it('calls onChange + onRunQuery when SPL editor blurs with new value', () => {
    const { onChange, onRunQuery } = setup();
    const editor = screen.getByTestId('code-editor') as HTMLTextAreaElement;
    fireEvent.input(editor, { target: { value: 'index=main error' } });
    fireEvent.blur(editor);
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ search: 'index=main error' }));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('parses maxResults as number', () => {
    const { onChange } = setup();
    fireEvent.change(screen.getByPlaceholderText('1000'), { target: { value: '500' } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ maxResults: 500 }));
  });

  it('clearing maxResults yields undefined', () => {
    // Empty-string input → parseInt returns NaN → maxResults becomes undefined.
    // (`type="number"` won't even fire a change event for non-numeric strings,
    // so the only path to undefined is clearing the field.)
    const { onChange } = setup({ maxResults: 500 });
    fireEvent.change(screen.getByPlaceholderText('1000'), { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ maxResults: undefined }));
  });

  it('accepts maxResults of 0 (unlimited)', () => {
    const { onChange } = setup();
    fireEvent.change(screen.getByPlaceholderText('1000'), { target: { value: '0' } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ maxResults: 0 }));
  });

  it('updates earliestTime', () => {
    const { onChange } = setup();
    const inputs = screen.getAllByPlaceholderText('(panel range)');
    fireEvent.change(inputs[0], { target: { value: '-15m' } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ earliestTime: '-15m' }));
  });

  it('updates latestTime', () => {
    const { onChange } = setup();
    const inputs = screen.getAllByPlaceholderText('(panel range)');
    fireEvent.change(inputs[1], { target: { value: 'now' } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ latestTime: 'now' }));
  });

  it('triggers onRunQuery on numeric blur', () => {
    const { onRunQuery } = setup();
    fireEvent.blur(screen.getByPlaceholderText('1000'));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('triggers onRunQuery on earliest blur', () => {
    const { onRunQuery } = setup();
    fireEvent.blur(screen.getAllByPlaceholderText('(panel range)')[0]);
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('preserves other query fields on a single change', () => {
    const { onChange } = setup({
      search: 's',
      maxResults: 100,
      earliestTime: '-1h',
      latestTime: 'now',
    });
    fireEvent.change(screen.getByPlaceholderText('1000'), { target: { value: '200' } });
    expect(onChange).toHaveBeenCalledWith({
      refId: 'A',
      search: 's',
      maxResults: 200,
      earliestTime: '-1h',
      latestTime: 'now',
    });
  });
});
