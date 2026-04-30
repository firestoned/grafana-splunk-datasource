import React, { ChangeEvent } from 'react';
import { InlineField, Input, SecretInput, InlineSwitch } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';

import { SplunkDataSourceOptions, SplunkSecureJsonData } from '../types';

type Props = DataSourcePluginOptionsEditorProps<SplunkDataSourceOptions, SplunkSecureJsonData>;

export function ConfigEditor(props: Props) {
  const { options, onOptionsChange } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onUrlChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, url: e.target.value },
    });
  };

  const onTlsSkipChange = (e: React.FormEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, tlsSkipVerify: e.currentTarget.checked },
    });
  };

  const onTokenChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { ...secureJsonData, authToken: e.target.value },
    });
  };

  const onTokenReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, authToken: false },
      secureJsonData: { ...secureJsonData, authToken: '' },
    });
  };

  return (
    <>
      <InlineField
        label="URL"
        labelWidth={20}
        tooltip="Splunk REST API base URL. For Splunk Cloud: https://<stack>.splunkcloud.com:8089. For on-prem: https://<host>:8089."
      >
        <Input
          width={50}
          value={jsonData.url ?? ''}
          placeholder="https://abc.splunkcloud.com:8089"
          onChange={onUrlChange}
        />
      </InlineField>
      <InlineField
        label="Auth Token"
        labelWidth={20}
        tooltip="A Splunk authentication token (NOT a HEC token). For Splunk Cloud: Settings → Tokens → New Token. The user owning the token needs read access to your indexes."
      >
        <SecretInput
          width={50}
          isConfigured={Boolean(secureJsonFields?.authToken)}
          value={secureJsonData?.authToken ?? ''}
          placeholder="eyJraWQiOiJzcGx1bmsuc2VjcmV0..."
          onChange={onTokenChange}
          onReset={onTokenReset}
        />
      </InlineField>
      <InlineField
        label="Skip TLS Verify"
        labelWidth={20}
        tooltip="Only enable for dev / self-signed certs. Leave OFF for Splunk Cloud."
      >
        <InlineSwitch value={Boolean(jsonData.tlsSkipVerify)} onChange={onTlsSkipChange} />
      </InlineField>
    </>
  );
}
