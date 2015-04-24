// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

/*
 * Mini Copy (C) 2015 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this fs except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"io/ioutil"

	"github.com/minio-io/mc/pkg/client"
	"github.com/minio-io/minio/pkg/iodine"
)

type fsClient struct {
	path string
}

// New - instantiate a new fs client
func New(path string) client.Client {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return &fsClient{path: path}
}

/// Object operations

// fsStat - wrapper function to get file stat
func (f *fsClient) fsStat() (os.FileInfo, error) {
	st, err := os.Stat(filepath.Clean(f.path))
	if os.IsNotExist(err) {
		return nil, iodine.New(FileNotFound{path: f.path}, nil)
	}
	if err != nil {
		return nil, iodine.New(err, nil)
	}
	return st, nil
}

// Get - download an object from bucket
func (f *fsClient) Get() (body io.ReadCloser, size int64, md5 string, err error) {
	item, err := f.getFSMetadata()
	if err != nil {
		return nil, 0, "", iodine.New(err, nil)
	}
	if item.FileType.IsDir() {
		return nil, 0, "", iodine.New(FileISDir{path: f.path}, nil)
	}
	body, err = os.Open(f.path)
	if err != nil {
		return nil, 0, "", iodine.New(err, nil)
	}
	// TODO: support md5sum - there is no easier way to do it right now without temporary buffer
	// so avoiding it to ensure no out of memory situations
	return body, item.Size, "", nil
}

// GetPartial - download a partial object from bucket
func (f *fsClient) GetPartial(offset, length int64) (body io.ReadCloser, size int64, md5 string, err error) {
	if offset < 0 {
		return nil, 0, "", iodine.New(client.InvalidRange{Offset: offset}, nil)
	}
	item, err := f.getFSMetadata()
	if err != nil {
		return nil, 0, "", iodine.New(err, nil)
	}
	if item.FileType.IsDir() {
		return nil, 0, "", iodine.New(FileISDir{path: f.path}, nil)
	}
	if offset > item.Size || (offset+length-1) > item.Size {
		return nil, 0, "", iodine.New(client.InvalidRange{Offset: offset}, nil)
	}
	_, err = io.CopyN(ioutil.Discard, body, offset)
	if err != nil {
		return nil, 0, "", iodine.New(err, nil)
	}
	return body, length, "", nil
}

func (f *fsClient) List() <-chan client.ItemOnChannel {
	itemCh := make(chan client.ItemOnChannel)
	go f.listInGoroutine(itemCh)
	return itemCh
}

func (f *fsClient) listInGoroutine(itemCh chan client.ItemOnChannel) {
	defer close(itemCh)
	visitFS := func(fp string, fi os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) { // skip inaccessible files
				return nil
			}
			return err // fatal
		}
		item := &client.Item{
			Name:     fp,
			Time:     fi.ModTime(),
			Size:     fi.Size(),
			FileType: fi.Mode(),
		}
		itemCh <- client.ItemOnChannel{
			Item: item,
			Err:  nil,
		}
		return nil
	}
	err := filepath.Walk(f.path, visitFS)
	if err != nil {
		itemCh <- client.ItemOnChannel{
			Item: nil,
			Err:  iodine.New(err, nil),
		}
	}
}

func isValidBucketACL(acl string) bool {
	switch acl {
	case "private":
		fallthrough
	case "public-read":
		fallthrough
	case "public-read-write":
		fallthrough
	case "":
		return true
	default:
		return false
	}
}

func aclToPerm(acl string) os.FileMode {
	switch acl {
	case "private":
		return os.FileMode(0700)
	case "public-read":
		return os.FileMode(0500)
	case "public-read-write":
		return os.FileMode(0777)
	default:
		return os.FileMode(0700)
	}
}

// PutBucket - create a new bucket
func (f *fsClient) PutBucket(acl string) error {
	if !isValidBucketACL(acl) {
		return iodine.New(errors.New("invalid acl"), nil)
	}
	err := os.MkdirAll(f.path, aclToPerm(acl))
	if err != nil {
		return iodine.New(err, nil)
	}
	err = os.Chmod(f.path, aclToPerm(acl))
	if err != nil {
		return iodine.New(err, nil)
	}
	return nil
}

// getFSMetadata -
func (f *fsClient) getFSMetadata() (item *client.Item, err error) {
	st, err := f.fsStat()
	if err != nil {
		return nil, iodine.New(err, nil)
	}
	item = new(client.Item)
	item.Name = st.Name()
	item.Size = st.Size()
	item.Time = st.ModTime()
	return item, nil
}

// Stat -
func (f *fsClient) Stat() (item *client.Item, err error) {
	return f.getFSMetadata()
}