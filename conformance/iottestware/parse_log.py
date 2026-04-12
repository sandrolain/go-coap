#!/usr/bin/env python3
"""parse_log.py – Eclipse Titan / IoT-Testware log → HTML + Markdown report.

Usage:
    python3 parse_log.py <log_file_or_dir> [output_dir]

If a directory is given, the newest *-mtc.log file found in it is used.
Reports are written alongside the log file unless output_dir is specified.
"""

import sys
import os
import re
import glob
import html as _html
from datetime import datetime
from typing import Optional, List, Dict, Any


# ── per-test descriptions ──────────────────────────────────────────────────────
TC_DESC: Dict[str, str] = {
    "TC_COAP_SERVER_001":
        "CON Ping – server must reply with RST for empty CON (code 0.00)",
    "TC_COAP_SERVER_HEADER_001":
        "RST with code 0.00 – server must NOT respond to an incoming RST",
    "TC_COAP_SERVER_HEADER_002":
        "Duplicate ACK deduplication – server must respond idempotently",
    "TC_COAP_SERVER_HEADER_003":
        "Unrecognized critical option – server must reply 4.02 Bad Option",
    "TC_COAP_SERVER_GET_001":
        "CON GET on default resource – server must reply 2.05 Content",
    "TC_COAP_SERVER_GET_002":
        "CON GET with ETag – server must reply 2.03 Valid on cache hit",
    "TC_COAP_SERVER_GET_003":
        "CON GET on nonexistent resource – server must reply 4.04 Not Found",
    "TC_COAP_SERVER_GET_005":
        "CON GET with Block2 option – server must support block-wise read",
    "TC_COAP_SERVER_POST_001":
        "CON POST creating new resource – server must reply 2.01 Created + Location-Path",
    "TC_COAP_SERVER_POST_002":
        "CON POST on existing resource – server must reply 2.04 Changed + Location-Path",
    "TC_COAP_SERVER_POST_003":
        "CON POST with Block1 – server must support block-wise write (RFC 7959)",
    "TC_COAP_SERVER_POST_005":
        "POST on method-not-allowed resource – server must reply 4.05",
    "TC_COAP_SERVER_PUT_001":
        "CON PUT on existing resource – server must reply 2.04 Changed",
    "TC_COAP_SERVER_PUT_002":
        "CON PUT creating new resource – server must reply 2.01 Created",
    "TC_COAP_SERVER_DELETE_001":
        "CON DELETE existing resource – server must reply 2.02 Deleted",
    "TC_COAP_SERVER_DELETE_002":
        "CON DELETE on storage resource – server must reply 2.02 Deleted",
    "TC_COAP_SEPERATE_RESPONSE_001":
        "Separate response – server sends empty ACK then deferred CON response",
    "TC_COAP_SEPERATE_RESPONSE_002":
        "Separate response retransmit – deferred CON retransmitted on timeout",
    "TC_COAP_SEPERATE_RESPONSE_003":
        "Separate response GET – empty ACK followed by separate CON response",
    "TC_COAP_SEPERATE_RESPONSE_004":
        "Separate response ACK – client acknowledges server's CON response",
    "TC_COAP_SERVER_NON_001":
        "NON GET – server must reply with NON 2.05 Content",
    "TC_COAP_SERVER_NON_002":
        "NON POST creating resource – server must reply with NON 2.01 Created + Location-Path",
    "TC_COAP_SERVER_NON_003":
        "NON PUT – server must reply with NON 2.04 Changed",
    "TC_COAP_SERVER_NON_004":
        "NON DELETE – server must reply with NON 2.02 Deleted",
}

TC_RFC: Dict[str, str] = {
    "TC_COAP_SERVER_001":           "RFC 7252 §4.3",
    "TC_COAP_SERVER_HEADER_001":    "RFC 7252 §4.2",
    "TC_COAP_SERVER_HEADER_002":    "RFC 7252 §4.5",
    "TC_COAP_SERVER_HEADER_003":    "RFC 7252 §5.4.1",
    "TC_COAP_SERVER_GET_001":       "RFC 7252 §5.8.1",
    "TC_COAP_SERVER_GET_002":       "RFC 7252 §5.10.6",
    "TC_COAP_SERVER_GET_003":       "RFC 7252 §5.8.1",
    "TC_COAP_SERVER_GET_005":       "RFC 7959 §2.2",
    "TC_COAP_SERVER_POST_001":      "RFC 7252 §5.8.2",
    "TC_COAP_SERVER_POST_002":      "RFC 7252 §5.8.2",
    "TC_COAP_SERVER_POST_003":      "RFC 7959 §2.1",
    "TC_COAP_SERVER_POST_005":      "RFC 7252 §5.8.2",
    "TC_COAP_SERVER_PUT_001":       "RFC 7252 §5.8.3",
    "TC_COAP_SERVER_PUT_002":       "RFC 7252 §5.8.3",
    "TC_COAP_SERVER_DELETE_001":    "RFC 7252 §5.8.4",
    "TC_COAP_SERVER_DELETE_002":    "RFC 7252 §5.8.4",
    "TC_COAP_SEPERATE_RESPONSE_001":"RFC 7252 §5.2.2",
    "TC_COAP_SEPERATE_RESPONSE_002":"RFC 7252 §5.2.2",
    "TC_COAP_SEPERATE_RESPONSE_003":"RFC 7252 §5.2.2",
    "TC_COAP_SEPERATE_RESPONSE_004":"RFC 7252 §5.2.2",
    "TC_COAP_SERVER_NON_001":       "RFC 7252 §5.2.3",
    "TC_COAP_SERVER_NON_002":       "RFC 7252 §5.2.3",
    "TC_COAP_SERVER_NON_003":       "RFC 7252 §5.2.3",
    "TC_COAP_SERVER_NON_004":       "RFC 7252 §5.2.3",
}

# Notes shown in the report for known failures / inconclusive results
TC_NOTES: Dict[str, str] = {
    "TC_COAP_SERVER_001": (
        "go-coap replied with an empty ACK (0.00) instead of a RST. "
        "RFC 7252 §4.3 states a server SHOULD reply with RST for an "
        "unrecognized empty CON. Fix: detect incoming CON with code=0.00 and "
        "respond with RST."
    ),
    "TC_COAP_SERVER_HEADER_001": (
        "go-coap sent a 4.04 Not Found response to an incoming RST. "
        "Servers MUST silently discard RST messages (RFC 7252 §4.2). "
        "Fix: ensure the server ignores RST frames entirely."
    ),
    "TC_COAP_SERVER_POST_001": (
        "SUT now correctly sends 2.01 Created + Location-Path:[New1,New2]. "
        "Still fails due to [BUG TITAN]: Titan template v_locationPaths includes "
        "a spurious option_length_ext byte for 4-byte options; go-coap sends "
        "option_length_ext:=omit (correct per RFC 7252 §3.1) → template mismatch."
    ),
    "TC_COAP_SERVER_POST_002": (
        "SUT sends 2.04 Changed + Location-Path:[New1,New2] (correct per TTCN source). "
        "Still fails due to [BUG TITAN]: same spurious option_length_ext mismatch "
        "in Titan response template as POST_001."
    ),
    "TC_COAP_SERVER_GET_002": (
        "No response received within the 2 s timer. The SUT likely does not "
        "implement ETag / conditional GET. Add ETag option handling to "
        "respond with 2.03 Valid when the ETag matches."
    ),
    "TC_COAP_SERVER_GET_005": (
        "No block-wise response received. go-coap handles block-wise transfer "
        "internally but the SUT resource must return a payload large enough to "
        "trigger Block2 splitting (> ~1024 bytes by default)."
    ),
    "TC_COAP_SERVER_POST_003": (
        "No response received during Block1 upload. The SUT handler must "
        "acknowledge each Block1 block with 2.31 Continue as required by "
        "RFC 7959 §2.1."
    ),
    "TC_COAP_SEPERATE_RESPONSE_001": (
        "Timer elapsed – empty ACK not received. The separate-response "
        "goroutine in the SUT may be sending the deferred response to the "
        "wrong address (NAT / Docker port translation). Ensure the SUT stores "
        "the client's source address from the incoming request and uses it for "
        "the outbound CON."
    ),
    "TC_COAP_SEPERATE_RESPONSE_002": (
        "Same root cause as SEPERATE_RESPONSE_001 – the initial empty ACK was "
        "not delivered."
    ),
    "TC_COAP_SEPERATE_RESPONSE_003": (
        "Same root cause as SEPERATE_RESPONSE_001."
    ),
    "TC_COAP_SEPERATE_RESPONSE_004": (
        "Same root cause as SEPERATE_RESPONSE_001."
    ),
    "TC_COAP_SERVER_NON_001": (
        "NON GET did not produce a NON response. go-coap may be replying with "
        "a CON ACK instead of a NON. Ensure the message type of the response "
        "mirrors the incoming NON type (RFC 7252 §5.2.3)."
    ),
    "TC_COAP_SERVER_NON_002": (
        "NON POST did not produce a NON response – same issue as NON_001."
    ),
    "TC_COAP_SERVER_NON_003": (
        "NON PUT did not produce a NON response – same issue as NON_001."
    ),
    "TC_COAP_SERVER_NON_004": (
        "NON DELETE did not produce a NON response – same issue as NON_001."
    ),
}


# ── timestamp helpers ──────────────────────────────────────────────────────────
_MONTHS = {
    'Jan': 1, 'Feb': 2, 'Mar': 3, 'Apr': 4,
    'May': 5, 'Jun': 6, 'Jul': 7, 'Aug': 8,
    'Sep': 9, 'Oct': 10, 'Nov': 11, 'Dec': 12,
}
_TS_RE = re.compile(r'(\d{4})/(\w{3})/(\d{2}) (\d{2}):(\d{2}):(\d{2})\.(\d+)')


def _parse_ts(s: str) -> Optional[datetime]:
    m = _TS_RE.match(s)
    if not m:
        return None
    yr, mon_s, dy, hr, mi, sc, us = m.groups()
    month = _MONTHS.get(mon_s)
    if not month:
        return None
    us_int = int(us.ljust(6, '0')[:6])
    try:
        return datetime(int(yr), month, int(dy), int(hr), int(mi), int(sc), us_int)
    except ValueError:
        return None


def _fmt_ts(dt: Optional[datetime]) -> str:
    if dt is None:
        return '—'
    return dt.strftime('%Y-%m-%d %H:%M:%S')


def _duration(start: Optional[datetime], end: Optional[datetime]) -> str:
    if start is None or end is None:
        return '—'
    secs = (end - start).total_seconds()
    return f'{secs:.3f} s'


# ── log parser ─────────────────────────────────────────────────────────────────
_LINE_RE   = re.compile(r'^(\d{4}/\w{3}/\d{2} \d{2}:\d{2}:\d{2}\.\d+) (\w+) ')
_TC_START  = re.compile(r'Test case (\S+) started\.')
_TC_FINISH = re.compile(r'Test case (\S+) finished\. Verdict: (\S+)(?: reason: (.+))?')
_SEND_RE   = re.compile(r'Encoding CoAP message: (.+)')
_RECV_RE   = re.compile(r'Decoded CoAP message: (.+)')
_STATS_RE  = re.compile(r'(\d+) test cases were executed\. Overall verdict: (\w+)')
_EXEC_RE   = re.compile(r'TTCN-3 Main Test Component started on (\S+?)\. Version: (.+)')


def parse_log(log_file: str) -> Dict[str, Any]:
    results: List[Dict[str, Any]] = []
    cur: Optional[Dict[str, Any]] = None
    overall_verdict: Optional[str] = None
    total_count = 0
    hostname = ''
    titan_version = ''
    run_start: Optional[datetime] = None
    run_end: Optional[datetime] = None

    with open(log_file, 'r', errors='replace') as fh:
        for raw in fh:
            line = raw.rstrip('\n')
            m = _LINE_RE.match(line)
            if not m:
                continue
            ts_str, etype = m.group(1), m.group(2)
            ts = _parse_ts(ts_str)
            if ts:
                if run_start is None:
                    run_start = ts
                run_end = ts
            rest = line[m.end():]

            if etype == 'EXECUTOR':
                em = _EXEC_RE.search(rest)
                if em:
                    hostname = em.group(1).rstrip('.')
                    titan_version = em.group(2).strip()

            elif etype == 'TESTCASE':
                sm = _TC_START.search(rest)
                if sm:
                    cur = {
                        'name': sm.group(1),
                        'start_ts': ts_str,
                        'start_dt': ts,
                        'end_ts': None,
                        'end_dt': None,
                        'verdict': None,
                        'reason': None,
                        'sent': [],
                        'received': [],
                    }
                    continue
                fm = _TC_FINISH.search(rest)
                if fm and cur and fm.group(1) == cur['name']:
                    cur['end_ts'] = ts_str
                    cur['end_dt'] = ts
                    cur['verdict'] = fm.group(2)
                    cur['reason'] = (fm.group(3) or '').strip() or None
                    results.append(cur)
                    cur = None

            elif etype == 'DEBUG' and cur is not None:
                sm2 = _SEND_RE.search(rest)
                if sm2:
                    cur['sent'].append(sm2.group(1))
                    continue
                rm = _RECV_RE.search(rest)
                if rm:
                    cur['received'].append(rm.group(1))

            elif etype == 'STATISTICS':
                sm3 = _STATS_RE.search(rest)
                if sm3:
                    total_count = int(sm3.group(1))
                    overall_verdict = sm3.group(2)

    return {
        'results': results,
        'overall_verdict': overall_verdict,
        'total_count': total_count,
        'hostname': hostname,
        'titan_version': titan_version,
        'run_start': run_start,
        'run_end': run_end,
        'log_file': log_file,
    }


# ── Markdown report ────────────────────────────────────────────────────────────

def _verdict_icon_md(v: str) -> str:
    return {'pass': '✅', 'fail': '❌', 'inconc': '⚠️'}.get(v.lower(), '❓')


def generate_markdown(data: Dict[str, Any]) -> str:
    results = data['results']
    counts: Dict[str, int] = {}
    for r in results:
        v = (r['verdict'] or 'none').lower()
        counts[v] = counts.get(v, 0) + 1

    total  = sum(counts.values())
    passed = counts.get('pass', 0)
    failed = counts.get('fail', 0)
    inconc = counts.get('inconc', 0)
    pct    = f'{passed / total * 100:.1f}' if total else '0.0'

    lines: List[str] = [
        '# CoAP Conformance Test Report',
        '',
        f'**SUT:** go-coap (github.com/plgd-dev/go-coap/v3)  ',
        f'**Test suite:** Eclipse IoT-Testware CoAP (TTCN-3)  ',
        f'**Run date:** {_fmt_ts(data["run_start"])}  ',
        f'**Duration:** {_duration(data["run_start"], data["run_end"])}  ',
        f'**Host:** {data["hostname"] or "—"}  ',
        f'**Titan version:** {data["titan_version"] or "—"}  ',
        '',
        '## Summary',
        '',
        f'| Metric | Value |',
        f'|--------|-------|',
        f'| Total  | {total} |',
        f'| ✅ Pass  | {passed} ({pct}%) |',
        f'| ❌ Fail  | {failed} |',
        f'| ⚠️ Inconclusive | {inconc} |',
        f'| Overall verdict | **{(data["overall_verdict"] or "—").upper()}** |',
        '',
        '## Results',
        '',
        '| # | Test Case | Description | RFC | Verdict | Duration | Reason |',
        '|---|-----------|-------------|-----|---------|----------|--------|',
    ]

    for i, r in enumerate(results, 1):
        v     = (r['verdict'] or 'none').lower()
        name  = r['name']
        desc  = TC_DESC.get(name, '—')
        rfc   = TC_RFC.get(name, '—')
        icon  = _verdict_icon_md(v)
        dur   = _duration(r['start_dt'], r['end_dt'])
        reason = r['reason'] or '—'
        lines.append(f'| {i} | `{name}` | {desc} | {rfc} | {icon} {v.upper()} | {dur} | {reason} |')

    lines += ['', '## Notes per test case', '']

    for r in results:
        v    = (r['verdict'] or 'none').lower()
        name = r['name']
        note = TC_NOTES.get(name)
        if note:
            icon = _verdict_icon_md(v)
            lines += [f'### {icon} {name}', '', note, '']

    lines += [
        '---',
        '',
        '_Report generated by parse_log.py_',
    ]

    return '\n'.join(lines)


# ── HTML report ────────────────────────────────────────────────────────────────

def _badge(verdict: str) -> str:
    v = verdict.lower()
    css = {'pass': 'badge-pass', 'fail': 'badge-fail', 'inconc': 'badge-inconc'}.get(v, 'badge-none')
    return f'<span class="badge {css}">{v.upper()}</span>'


def _escape(s: str) -> str:
    return _html.escape(s)


def generate_html(data: Dict[str, Any]) -> str:
    results = data['results']
    counts: Dict[str, int] = {}
    for r in results:
        v = (r['verdict'] or 'none').lower()
        counts[v] = counts.get(v, 0) + 1

    total  = sum(counts.values())
    passed = counts.get('pass', 0)
    failed = counts.get('fail', 0)
    inconc = counts.get('inconc', 0)
    pass_pct = f'{passed / total * 100:.1f}' if total else '0.0'

    # ── table rows ──────────────────────────────────────────────────────────
    rows_html: List[str] = []
    for i, r in enumerate(results):
        v     = (r['verdict'] or 'none').lower()
        name  = r['name']
        desc  = TC_DESC.get(name, '—')
        rfc   = TC_RFC.get(name, '—')
        dur   = _duration(r['start_dt'], r['end_dt'])
        reason = _escape(r['reason'] or '—')
        note  = TC_NOTES.get(name, '')
        row_cls = {'pass': 'row-pass', 'fail': 'row-fail', 'inconc': 'row-inconc'}.get(v, '')

        sent_items  = ''.join(f'<li><code>{_escape(m)}</code></li>' for m in r.get('sent', []))
        recv_items  = ''.join(f'<li><code>{_escape(m)}</code></li>' for m in r.get('received', []))
        sent_html   = f'<ul>{sent_items}</ul>' if sent_items else '<em>none</em>'
        recv_html   = f'<ul>{recv_items}</ul>' if recv_items else '<em>none</em>'
        note_html   = f'<div class="note"><strong>Analysis:</strong> {_escape(note)}</div>' if note else ''
        detail_id   = f'detail-{i}'

        rows_html.append(f'''
        <tr class="{row_cls}" onclick="toggleDetail('{detail_id}')">
          <td class="tc-num">{i + 1}</td>
          <td class="tc-name">{_escape(name)}</td>
          <td class="tc-desc">{_escape(desc)}</td>
          <td class="tc-rfc">{_escape(rfc)}</td>
          <td class="tc-verdict">{_badge(v)}</td>
          <td class="tc-dur">{dur}</td>
          <td class="tc-reason">{reason}</td>
        </tr>
        <tr id="{detail_id}" class="detail-row hidden">
          <td colspan="7">
            <div class="detail-box">
              <div class="msg-col">
                <h4>&#8593; Sent</h4>
                {sent_html}
              </div>
              <div class="msg-col">
                <h4>&#8595; Received</h4>
                {recv_html}
              </div>
            </div>
            {note_html}
          </td>
        </tr>''')

    table_body = '\n'.join(rows_html)

    # ── overall verdict color ────────────────────────────────────────────────
    ov = (data['overall_verdict'] or 'none').lower()
    ov_cls = {'pass': 'badge-pass', 'fail': 'badge-fail', 'inconc': 'badge-inconc'}.get(ov, 'badge-none')

    html_doc = f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>CoAP Conformance Report</title>
  <style>
    *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
    body {{
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #f5f7fa;
      color: #333;
      padding: 2rem;
    }}
    h1 {{ font-size: 1.8rem; margin-bottom: 0.25rem; color: #1a1a2e; }}
    h2 {{ font-size: 1.2rem; margin: 2rem 0 0.75rem; color: #1a1a2e; border-bottom: 2px solid #e2e8f0; padding-bottom: 0.4rem; }}
    h4 {{ font-size: 0.85rem; margin-bottom: 0.4rem; color: #555; text-transform: uppercase; letter-spacing: 0.05em; }}
    .meta {{ color: #666; font-size: 0.875rem; margin-bottom: 1.5rem; }}
    .meta span {{ margin-right: 1.5rem; }}

    /* summary cards */
    .cards {{ display: flex; gap: 1rem; flex-wrap: wrap; margin-bottom: 2rem; }}
    .card {{
      flex: 1; min-width: 130px; padding: 1rem 1.25rem;
      border-radius: 10px; background: #fff;
      box-shadow: 0 1px 4px rgba(0,0,0,.08);
      text-align: center;
    }}
    .card .num {{ font-size: 2.2rem; font-weight: 700; line-height: 1; }}
    .card .lbl {{ font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.06em; color: #888; margin-top: 0.3rem; }}
    .card.c-total .num  {{ color: #334155; }}
    .card.c-pass  .num  {{ color: #16a34a; }}
    .card.c-fail  .num  {{ color: #dc2626; }}
    .card.c-inconc .num {{ color: #d97706; }}

    /* progress bar */
    .progress-wrap {{ background: #e2e8f0; border-radius: 6px; height: 12px; overflow: hidden; margin-bottom: 2rem; }}
    .progress-bar  {{ height: 100%; border-radius: 6px; transition: width .4s; }}
    .progress-bar.pb-pass   {{ background: #16a34a; }}
    .progress-bar.pb-mixed  {{ background: linear-gradient(90deg,#16a34a {pass_pct}%,#dc2626 {pass_pct}%); }}

    /* badges */
    .badge {{ display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 0.72rem; font-weight: 700; letter-spacing: 0.04em; }}
    .badge-pass  {{ background: #dcfce7; color: #166534; }}
    .badge-fail  {{ background: #fee2e2; color: #991b1b; }}
    .badge-inconc{{ background: #fef3c7; color: #92400e; }}
    .badge-none  {{ background: #f1f5f9; color: #475569; }}

    /* table */
    table {{ width: 100%; border-collapse: collapse; background: #fff;
             border-radius: 10px; overflow: hidden;
             box-shadow: 0 1px 4px rgba(0,0,0,.08); }}
    thead {{ background: #1e293b; color: #e2e8f0; }}
    thead th {{ padding: 0.6rem 0.75rem; text-align: left; font-size: 0.78rem;
                text-transform: uppercase; letter-spacing: 0.05em; }}
    tbody tr {{ border-bottom: 1px solid #f1f5f9; cursor: pointer; }}
    tbody tr:hover {{ background: #f8fafc; }}
    td {{ padding: 0.55rem 0.75rem; font-size: 0.85rem; vertical-align: middle; }}
    .tc-num   {{ width: 2.5rem; color: #94a3b8; text-align: center; }}
    .tc-name  {{ font-family: monospace; font-size: 0.8rem; white-space: nowrap; }}
    .tc-desc  {{ color: #475569; }}
    .tc-rfc   {{ white-space: nowrap; font-size: 0.78rem; color: #64748b; }}
    .tc-verdict{{ text-align: center; white-space: nowrap; }}
    .tc-dur   {{ white-space: nowrap; color: #64748b; font-size: 0.78rem; text-align: right; }}
    .tc-reason{{ font-size: 0.78rem; color: #64748b; max-width: 200px; }}

    .row-pass  td:first-child {{ border-left: 3px solid #16a34a; }}
    .row-fail  td:first-child {{ border-left: 3px solid #dc2626; }}
    .row-inconc td:first-child {{ border-left: 3px solid #d97706; }}

    /* detail rows */
    .detail-row td {{ background: #f8fafc; padding: 0.75rem 1.25rem; cursor: default; }}
    .detail-box {{ display: flex; gap: 2rem; flex-wrap: wrap; }}
    .msg-col {{ flex: 1; min-width: 280px; }}
    .msg-col ul {{ list-style: none; padding: 0; }}
    .msg-col li {{ margin-bottom: 0.3rem; }}
    .msg-col code {{
      display: block; background: #1e293b; color: #e2e8f0;
      padding: 0.35rem 0.6rem; border-radius: 4px;
      font-size: 0.75rem; word-break: break-all; white-space: pre-wrap;
    }}
    .note {{
      margin-top: 0.75rem;
      background: #fffbeb; border-left: 4px solid #d97706;
      padding: 0.5rem 0.75rem; border-radius: 0 6px 6px 0;
      font-size: 0.82rem; color: #451a03;
    }}
    .hidden {{ display: none; }}

    footer {{ margin-top: 2rem; font-size: 0.75rem; color: #94a3b8; text-align: center; }}
  </style>
</head>
<body>
  <h1>&#128203; CoAP Conformance Test Report</h1>
  <div class="meta">
    <span><strong>SUT:</strong> go-coap (github.com/plgd-dev/go-coap/v3)</span>
    <span><strong>Suite:</strong> Eclipse IoT-Testware CoAP (TTCN-3)</span>
    <span><strong>Run:</strong> {_fmt_ts(data['run_start'])}</span>
    <span><strong>Duration:</strong> {_duration(data['run_start'], data['run_end'])}</span>
    <span><strong>Host:</strong> {_escape(data['hostname'] or '—')}</span>
    <span><strong>Titan:</strong> {_escape(data['titan_version'] or '—')}</span>
    <span><strong>Overall:</strong> <span class="badge {ov_cls}">{ov.upper()}</span></span>
  </div>

  <div class="cards">
    <div class="card c-total">
      <div class="num">{total}</div>
      <div class="lbl">Total</div>
    </div>
    <div class="card c-pass">
      <div class="num">{passed}</div>
      <div class="lbl">Passed ({pass_pct}%)</div>
    </div>
    <div class="card c-fail">
      <div class="num">{failed}</div>
      <div class="lbl">Failed</div>
    </div>
    <div class="card c-inconc">
      <div class="num">{inconc}</div>
      <div class="lbl">Inconclusive</div>
    </div>
  </div>

  <div class="progress-wrap">
    <div class="progress-bar pb-mixed" style="width:100%"></div>
  </div>

  <h2>Test Results</h2>
  <p style="font-size:0.8rem;color:#64748b;margin-bottom:0.75rem">
    Click any row to expand message details.
  </p>
  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>Test Case</th>
        <th>Description</th>
        <th>RFC Ref</th>
        <th>Verdict</th>
        <th>Duration</th>
        <th>Reason</th>
      </tr>
    </thead>
    <tbody>
      {table_body}
    </tbody>
  </table>

  <footer>
    Report generated by <code>parse_log.py</code> &mdash;
    Log: <code>{_escape(os.path.basename(data['log_file']))}</code>
  </footer>

  <script>
    function toggleDetail(id) {{
      const el = document.getElementById(id);
      if (el) el.classList.toggle('hidden');
    }}
  </script>
</body>
</html>'''

    return html_doc


# ── entrypoint ─────────────────────────────────────────────────────────────────

def _find_latest_log(directory: str) -> Optional[str]:
    # Titan names the main test log <suite>.<host>-mtc.log
    for pattern in ('*-mtc.log', '*.log'):
        files = glob.glob(os.path.join(directory, pattern))
        if files:
            return max(files, key=os.path.getmtime)
    return None


def main() -> int:
    args = sys.argv[1:]
    if not args:
        print(f'Usage: {sys.argv[0]} <log_file_or_dir> [output_dir]', file=sys.stderr)
        return 1

    src = args[0]
    if os.path.isdir(src):
        log_file = _find_latest_log(src)
        if not log_file:
            print(f'No *.mtc.log files found in {src}', file=sys.stderr)
            return 1
    elif os.path.isfile(src):
        log_file = src
    else:
        print(f'Not found: {src}', file=sys.stderr)
        return 1

    output_dir = args[1] if len(args) > 1 else os.path.dirname(log_file)
    os.makedirs(output_dir, exist_ok=True)

    base = os.path.splitext(os.path.basename(log_file))[0]
    html_out = os.path.join(output_dir, base + '.html')
    md_out   = os.path.join(output_dir, base + '.md')

    print(f'[parse_log] Parsing: {log_file}')
    data = parse_log(log_file)

    total   = data['total_count'] or len(data['results'])
    counts: Dict[str, int] = {}
    for r in data['results']:
        v = (r['verdict'] or 'none').lower()
        counts[v] = counts.get(v, 0) + 1

    passed = counts.get('pass', 0)
    failed = counts.get('fail', 0)
    inconc = counts.get('inconc', 0)
    print(f'[parse_log] Results: {total} total, {passed} pass, {failed} fail, {inconc} inconc')
    print(f'[parse_log] Overall verdict: {data["overall_verdict"] or "—"}')

    html_content = generate_html(data)
    with open(html_out, 'w', encoding='utf-8') as fh:
        fh.write(html_content)
    print(f'[parse_log] HTML report: {html_out}')

    md_content = generate_markdown(data)
    with open(md_out, 'w', encoding='utf-8') as fh:
        fh.write(md_content)
    print(f'[parse_log] Markdown report: {md_out}')

    return 0


if __name__ == '__main__':
    sys.exit(main())
