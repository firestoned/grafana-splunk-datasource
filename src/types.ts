import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

/**
 * A single Splunk search query.
 *
 * `search` is raw SPL. The leading `search` command is implicit — i.e. you
 * can write `index=main error` and the backend will prepend `search ` if
 * the query doesn't start with a command.
 */
export interface SplunkQuery extends DataQuery {
  /** SPL search string, e.g. `index=main level=ERROR | head 100`. */
  search: string;
  /** Max result count. Maps to Splunk's `count` param. 0 = unlimited (use with care). */
  maxResults?: number;
  /** Optional override; otherwise panel time range is used. e.g. "-15m" or "2024-04-01T00:00:00". */
  earliestTime?: string;
  /** Optional override; otherwise panel time range is used. e.g. "now". */
  latestTime?: string;
}

export const DEFAULT_QUERY: Partial<SplunkQuery> = {
  search: '',
  maxResults: 1000,
};

/**
 * Non-secret data source configuration. Stored as JSON in Grafana's database.
 */
export interface SplunkDataSourceOptions extends DataSourceJsonData {
  /**
   * Splunk REST API base URL. For Splunk Cloud this is typically
   * `https://<stack>.splunkcloud.com:8089`. For on-prem it might be
   * `https://splunk.example.com:8089`.
   */
  url: string;
  /** Skip TLS verification (self-signed certs on dev/on-prem only). Default false. */
  tlsSkipVerify?: boolean;
}

/**
 * Secret configuration. Encrypted at rest by Grafana, never returned to the
 * frontend after save.
 */
export interface SplunkSecureJsonData {
  /**
   * Splunk authentication token (NOT a HEC token). Sent as
   * `Authorization: Bearer <token>`. See README for how to mint one.
   */
  authToken?: string;
}
