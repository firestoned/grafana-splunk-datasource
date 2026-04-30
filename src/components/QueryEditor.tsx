import React, { ChangeEvent } from 'react';
import { InlineField, Input, CodeEditor } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';

import { DataSource } from '../datasource';
import { SplunkQuery, SplunkDataSourceOptions } from '../types';

type Props = QueryEditorProps<DataSource, SplunkQuery, SplunkDataSourceOptions>;

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const onSearchChange = (value: string) => {
    onChange({ ...query, search: value });
  };

  const onMaxChange = (e: ChangeEvent<HTMLInputElement>) => {
    const n = parseInt(e.target.value, 10);
    onChange({ ...query, maxResults: Number.isFinite(n) ? n : undefined });
  };

  const onEarliestChange = (e: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, earliestTime: e.target.value });
  };

  const onLatestChange = (e: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, latestTime: e.target.value });
  };

  const { search, maxResults, earliestTime, latestTime } = query;

  return (
    <div className="gf-form-group">
      <InlineField
        label="SPL"
        labelWidth={20}
        grow
        tooltip="Splunk Search Processing Language. The leading `search` command is optional — `index=main error` is fine."
      >
        <div style={{ width: '100%', minHeight: 120 }}>
          <CodeEditor
            value={search ?? ''}
            language="sql"
            height={120}
            showLineNumbers={true}
            showMiniMap={false}
            onBlur={(v) => {
              onSearchChange(v);
              onRunQuery();
            }}
            onSave={(v) => {
              onSearchChange(v);
              onRunQuery();
            }}
            monacoOptions={{ wordWrap: 'on', fontSize: 13 }}
          />
        </div>
      </InlineField>
      <InlineField
        label="Max results"
        labelWidth={20}
        tooltip="0 = unlimited. Default 1000. Splunk's `count` parameter."
      >
        <Input
          width={20}
          type="number"
          value={maxResults ?? ''}
          placeholder="1000"
          onChange={onMaxChange}
          onBlur={onRunQuery}
        />
      </InlineField>
      <InlineField
        label="Earliest"
        labelWidth={20}
        tooltip='Optional override for the panel time range. Splunk relative time, e.g. "-15m", "-1h@h", or absolute "2024-04-01T00:00:00".'
      >
        <Input
          width={30}
          value={earliestTime ?? ''}
          placeholder="(panel range)"
          onChange={onEarliestChange}
          onBlur={onRunQuery}
        />
      </InlineField>
      <InlineField label="Latest" labelWidth={20} tooltip='Optional. e.g. "now".'>
        <Input
          width={30}
          value={latestTime ?? ''}
          placeholder="(panel range)"
          onChange={onLatestChange}
          onBlur={onRunQuery}
        />
      </InlineField>
    </div>
  );
}
