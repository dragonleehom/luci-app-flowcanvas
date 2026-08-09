'use strict';
'require view';

return view.extend({
	render: function() {
		var iframe = E('iframe', {
			src: L.env.resource + '/flowcanvas/index.html',
			style: 'width: 100%; height: 100%; min-height: 800px; border: none; overflow: hidden; background: #10213f;'
		});
		return E('div', { class: 'cbi-map' }, [
			E('div', { class: 'cbi-section' }, [
				iframe
			])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});
