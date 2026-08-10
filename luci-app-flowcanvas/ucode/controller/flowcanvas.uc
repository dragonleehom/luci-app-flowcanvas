'use strict';

import { stdout } from 'fs';
import * as socket from 'socket';
import { cursor } from 'uci';

const BLOCKSIZE = 32776;
const ROUTE_PREFIX = '/admin/services/flowcanvas/api/v1';

function error_response(code, message) {
	http.status(code, message);
	http.prepare_content('application/json');
	http.write_json({ error: message });
}

function get_api_port() {
	let ctx = cursor();
	let configured_port = ctx.get_first('flowcanvas', 'main', 'api_port') || '16789';
	ctx.unload();
	if (!match(configured_port, /^\d{2,5}$/))
		return null;
	let port = int(configured_port);
	return port >= 1024 && port <= 65535 ? port : null;
}

function get_api_path() {
	let path_info = http.getenv('PATH_INFO') || '';
	if (index(path_info, ROUTE_PREFIX) !== 0)
		return null;
	let api_path = substr(path_info, length(ROUTE_PREFIX));
	return api_path != '' ? api_path : null;
}

function is_read_path_allowed(api_path) {
	return api_path == '/health' ||
		api_path == '/canvas' ||
		api_path == '/targets' ||
		api_path == '/features' ||
		match(api_path, /^\/compilations\/[a-z0-9-]+$/);
}

function is_write_path_allowed(api_path) {
	return api_path == '/canvas/graph' ||
		api_path == '/discovery/refresh' ||
		api_path == '/compilations/validate' ||
		api_path == '/compilations/apply';
}

function proxy_request(api_path, upstream_method) {
	let port = get_api_port();
	if (!port) {
		error_response(500, 'Invalid FlowCanvas loopback port configuration');
		return;
	}

	let sock_info = socket.addrinfo('127.0.0.1', port, { protocol: socket.IPPROTO_TCP });
	if (!sock_info || !sock_info[0]) {
		error_response(502, 'Could not resolve FlowCanvas backend address');
		return;
	}

	let sock = socket.create(socket.AF_INET, socket.SOCK_STREAM);
	if (!sock || !sock.connect(socket.sockaddr(sock_info[0].addr))) {
		if (sock) sock.close();
		error_response(503, 'FlowCanvas backend is not running');
		return;
	}

	let query_str = http.getenv('QUERY_STRING');
	let target_url = `/api/v1${api_path}`;
	if (query_str && query_str != '')
		target_url += `?${query_str}`;

	let req = [
		`${upstream_method} ${target_url} HTTP/1.0`,
		`Host: 127.0.0.1:${port}`,
		'User-Agent: luci-app-flowcanvas-proxy',
		'Connection: close'
	];

	let if_match = http.getenv('HTTP_IF_MATCH');
	if (if_match)
		push(req, `If-Match: ${if_match}`);

	let content_len = int(http.getenv('CONTENT_LENGTH') || 0);
	let content_type = http.getenv('CONTENT_TYPE');
	if (content_len < 0 || content_len > 1048576) {
		sock.close();
		error_response(413, 'FlowCanvas request body exceeds 1 MiB');
		return;
	}
	if (content_len > 0) {
		push(req, `Content-Length: ${content_len}`);
		if (content_type)
			push(req, `Content-Type: ${content_type}`);
	}

	push(req, '');
	push(req, '');
	sock.send(join('\r\n', req));

	if (content_len > 0) {
		let body = http.content();
		if (body)
			sock.send(body);
	}

	let response_buff = sock.recv(BLOCKSIZE);
	if (!response_buff || response_buff == '') {
		sock.close();
		error_response(502, 'No response from FlowCanvas backend');
		return;
	}

	let parts = split(response_buff, /\r?\n\r?\n/, 2);
	let header_lines = split(parts[0], /\r?\n/);
	let status_match = match(header_lines[0], /HTTP\/\S+\s+(\d+)\s+(.*)/);
	if (status_match)
		http.status(int(status_match[1]), status_match[2]);
	else
		http.status(502, 'Invalid backend response');

	for (let i = 1; i < length(header_lines); i++) {
		let line = header_lines[i];
		let colon = index(line, ':');
		if (colon <= 0)
			continue;
		let key = lc(trim(substr(line, 0, colon)));
		let value = trim(substr(line, colon + 1));
		if (key != 'connection' && key != 'transfer-encoding' && key != 'content-length')
			http.header(key, value);
	}

	http.write_headers();
	if (length(parts) > 1 && parts[1] != '')
		stdout.write(parts[1]);

	let chunk;
	while ((chunk = sock.recv(BLOCKSIZE))) {
		if (chunk && length(chunk) > 0)
			stdout.write(chunk);
	}
	sock.close();
}

function api_read(...args) {
	if (http.getenv('REQUEST_METHOD') != 'GET') {
		http.status(405, 'Method Not Allowed');
		http.header('Allow', 'GET');
		return;
	}
	let api_path = get_api_path();
	if (!api_path || !is_read_path_allowed(api_path)) {
		error_response(404, 'Unknown FlowCanvas read endpoint');
		return;
	}
	proxy_request(api_path, 'GET');
}

function api_write(...args) {
	// The LuCI dispatcher has already enforced POST + session token for this action.
	let api_path = get_api_path();
	if (!api_path || !is_write_path_allowed(api_path)) {
		error_response(404, 'Unknown FlowCanvas write endpoint');
		return;
	}
	let method = api_path == '/canvas/graph' ? 'PUT' : 'POST';
	proxy_request(api_path, method);
}

return {
	api_read,
	api_write
};
