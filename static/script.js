document.getElementById('uploadForm').addEventListener('submit', function(e) {
    e.preventDefault();
    var fileInput = document.getElementById('fileInput');
    var file = fileInput.files[0];

    var formData = new FormData();
    formData.append('file', file);

    fetch('/upload', {
        method: 'POST',
        body: formData
    }).then(response => response.text()).then(data => {
        console.log(data);
        loadFiles();
    }).catch(error => {
        console.error('Error:', error);
    });
});

function loadFiles() {
    fetch('/files').then(response => response.text()).then(data => {
        var fileList = document.getElementById('fileList');
        fileList.innerHTML = '';
        data.split('\n').forEach(file => {
            if (file) {
                var li = document.createElement('li');
                li.textContent = file;

                var downloadLink = document.createElement('a');
                downloadLink.href = '/download/' + file;
                downloadLink.textContent = ' Download';
                li.appendChild(downloadLink);

                var deleteButton = document.createElement('button');
                deleteButton.textContent = ' Delete';
                deleteButton.addEventListener('click', function() {
                    deleteFile(file);
                });

                li.appendChild(deleteButton);
                fileList.appendChild(li);
            }
        });
    }).catch(error => {
        console.error('Error:', error);
    });
}

function deleteFile(fileName) {
    fetch('/delete/' + fileName, {
        method: 'DELETE'
    }).then(response => {
        if (response.ok) {
            console.log('File deleted:', fileName);
            loadFiles(); // 重新加载文件列表
        } else {
            console.error('Failed to delete file:', fileName);
        }
    }).catch(error => {
        console.error('Error:', error);
    });
}

// 初始化加载文件列表
loadFiles();
