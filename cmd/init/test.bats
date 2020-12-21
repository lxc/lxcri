#!/usr/bin/env bats

bin=$BATS_TEST_DIRNAME/crio-lxc-init

cd_tmpdir () {
  cd $BATS_TEST_DIRNAME
}

myfoo() {
  cd $BATS_TEST_DIRNAME
  [ -f environ ] && rm environ
  [ -f cmdline ] && rm cmdline
  [ -f syncfifo ] && rm syncfifo
}

#setup cd_tmpdir
teardown myfoo


@test "noon-existent environment file" {
  #cd $BATS_TEST_DIRNAME
  run $bin 12345
  echo $status
  [ "$status" -eq 210 ]
}

@test "non-existent environment file" {
  #cd $BATS_TEST_DIRNAME
  run $bin 12345 
  [ "$status" -eq 210 ]
}
