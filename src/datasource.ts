import { DataSourceInstanceSettings, CoreApp } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { SplunkQuery, SplunkDataSourceOptions, DEFAULT_QUERY } from './types';

/**
 * Frontend DataSource. Backend (Go) does all the actual Splunk REST calls;
 * we never put the token in the browser.
 */
export class DataSource extends DataSourceWithBackend<SplunkQuery, SplunkDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<SplunkDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<SplunkQuery> {
    return DEFAULT_QUERY;
  }
}
