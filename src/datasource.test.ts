jest.mock('@grafana/runtime', () => ({
  DataSourceWithBackend: class {
    instanceSettings: unknown;
    constructor(instanceSettings: unknown) {
      this.instanceSettings = instanceSettings;
    }
  },
}));

import { CoreApp, DataSourceInstanceSettings } from '@grafana/data';

import { DataSource } from './datasource';
import { DEFAULT_QUERY, SplunkDataSourceOptions } from './types';

const baseSettings = {
  id: 1,
  uid: 'test',
  type: 'firestoned-splunk-datasource',
  name: 'test-ds',
  meta: {},
  readOnly: false,
  access: 'proxy',
  jsonData: { url: 'https://splunk.example.com:8089' },
} as unknown as DataSourceInstanceSettings<SplunkDataSourceOptions>;

describe('DataSource', () => {
  it('constructs without error', () => {
    expect(() => new DataSource(baseSettings)).not.toThrow();
  });

  it('getDefaultQuery returns DEFAULT_QUERY', () => {
    const ds = new DataSource(baseSettings);
    expect(ds.getDefaultQuery(CoreApp.PanelEditor)).toEqual(DEFAULT_QUERY);
  });

  it('getDefaultQuery is independent of CoreApp value', () => {
    const ds = new DataSource(baseSettings);
    expect(ds.getDefaultQuery(CoreApp.Dashboard)).toEqual(DEFAULT_QUERY);
    expect(ds.getDefaultQuery(CoreApp.Explore)).toEqual(DEFAULT_QUERY);
  });
});

describe('DEFAULT_QUERY', () => {
  it('has the expected shape with sensible defaults', () => {
    expect(DEFAULT_QUERY).toEqual({
      search: '',
      maxResults: 1000,
    });
  });
});
