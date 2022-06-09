#!/usr/bin/env python

# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from concurrent.futures import ThreadPoolExecutor
import multiprocessing
import os
import random
import shutil
import string
import subprocess
import sys
import time
import yaml

def editWork(y, num):
    test_name = 'test-work-' + ''.join(random.choices(string.ascii_lowercase, k=10))
    y['metadata']['name'] = test_name
    y['spec']['workload']['manifests'][0]['metadata']['name'] = test_name

    modified_name = "modified" + str(num) + ".yaml"
    with open(modified_name, "w") as output:
        yaml.dump(y, output, default_flow_style=False, sort_keys=False)

    subprocess.run(["kubectl", "apply", "-f", modified_name])
    output.close()
    os.remove(modified_name)

def main():
    with open("example-work.yaml") as f:
        y = yaml.safe_load(f)

    batch_size = 25
    skip_counter = 0
    batch_num = int(int(sys.argv[1]) / batch_size)
    if batch_num > 0:
        for i in range(0, batch_num - 1):
            processes = []
            for i in range(0, batch_size):
                try:
                    str_error = None
                    p = multiprocessing.Process(target = editWork, args = (y,i,))
                    p.start()
                    processes.append(p)
                except Exception as e:
                    skip_counter += 1
                    pass
            for p in processes:
                p.join()

    for i in range(0, int(sys.argv[1]) % batch_size):
        p = multiprocessing.Process(target = editWork, args = (y,i,))
        p.start()
        processes.append(p)

    for p in processes:
        p.join()

    print("final skip counter is" + str(skip_counter))

if __name__ == "__main__":
    main()
