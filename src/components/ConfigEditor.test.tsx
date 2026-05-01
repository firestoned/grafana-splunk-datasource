import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';

import { ConfigEditor } from './ConfigEditor';

function makeOptions(overrides: any = {}): any {
  return {
    id: 1,
    uid: 'test',
    orgId: 1,
    name: 'test-ds',
    type: 'firestoned-splunk-datasource',
    typeName: 'Splunk',
    typeLogoUrl: '',
    access: 'proxy',
    url: '',
    user: '',
    database: '',
    basicAuth: false,
    basicAuthUser: '',
    withCredentials: false,
    isDefault: false,
    version: 1,
    readOnly: false,
    jsonData: { url: '' },
    secureJsonFields: {},
    secureJsonData: {},
    ...overrides,
  };
}

function setup(overrides: any = {}) {
  const onOptionsChange = jest.fn();
  render(<ConfigEditor options={makeOptions(overrides)} onOptionsChange={onOptionsChange} />);
  return { onOptionsChange };
}

describe('ConfigEditor', () => {
  it('renders URL and token inputs', () => {
    setup();
    expect(screen.getByPlaceholderText(/splunkcloud/)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/eyJraWQ/)).toBeInTheDocument();
  });

  it('shows existing URL', () => {
    setup({ jsonData: { url: 'https://abc.splunkcloud.com:8089' } });
    expect(screen.getByDisplayValue('https://abc.splunkcloud.com:8089')).toBeInTheDocument();
  });

  it('updates URL', () => {
    const { onOptionsChange } = setup();
    fireEvent.change(screen.getByPlaceholderText(/splunkcloud/), {
      target: { value: 'https://abc.splunkcloud.com:8089' },
    });
    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: { url: 'https://abc.splunkcloud.com:8089' },
      })
    );
  });

  it('preserves jsonData fields when URL changes', () => {
    const { onOptionsChange } = setup({
      jsonData: { url: 'old', other: 'kept' } as any,
    });
    fireEvent.change(screen.getByPlaceholderText(/splunkcloud/), {
      target: { value: 'new' },
    });
    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({ url: 'new', other: 'kept' }),
      })
    );
  });

  it('updates auth token', () => {
    const { onOptionsChange } = setup();
    fireEvent.change(screen.getByPlaceholderText(/eyJraWQ/), {
      target: { value: 'eyJraWQ.SECRET' },
    });
    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        secureJsonData: { authToken: 'eyJraWQ.SECRET' },
      })
    );
  });

  it('shows reset affordance when token is configured', () => {
    setup({ secureJsonFields: { authToken: true } });
    expect(screen.getByText(/reset/i)).toBeInTheDocument();
  });

  it('reset clears authToken and the configured flag', () => {
    const { onOptionsChange } = setup({ secureJsonFields: { authToken: true } });
    fireEvent.click(screen.getByText(/reset/i));
    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        secureJsonFields: { authToken: false },
        secureJsonData: { authToken: '' },
      })
    );
  });
});
