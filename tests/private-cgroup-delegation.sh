# Source only from PID 1 in the owned, bounded --cgroupns=private unit container.
test "${FACTORYBOX_UPSTREAM_CONTAINER:-}" = 1
test "$$" = 1
test "$(cat /proc/1/cgroup)" = '0::/'
factorybox_memory_bound=$(cat /sys/fs/cgroup/memory.max)
case "$factorybox_memory_bound" in
  ''|*[!0-9]*) exit 1 ;;
esac
test "$factorybox_memory_bound" -le 3221225472
test "$(cat /sys/fs/cgroup/cgroup.type)" = domain
factorybox_controller_group=/sys/fs/cgroup/factorybox-unit-controller
test ! -e "$factorybox_controller_group"
mkdir "$factorybox_controller_group"
printf '%s\n' "$$" > "$factorybox_controller_group/cgroup.procs"
# The no-internal-process rule applies to this private root. All future
# compiler, daemon and test processes inherit the controller leaf instead.
if IFS= read -r factorybox_unexpected_pid < /sys/fs/cgroup/cgroup.procs; then
  printf 'private cgroup root still contains a process: %s\n' "$factorybox_unexpected_pid" >&2
  exit 1
fi
factorybox_controllers=$(cat /sys/fs/cgroup/cgroup.controllers)
test -n "$factorybox_controllers"
factorybox_enable=''
for factorybox_controller in $factorybox_controllers; do
  factorybox_enable="$factorybox_enable +$factorybox_controller"
done
printf '%s\n' "$factorybox_enable" > /sys/fs/cgroup/cgroup.subtree_control
printf 'private root type=%s controllers=%s process-group=%s\n' \
  "$(cat /sys/fs/cgroup/cgroup.type)" \
  "$(cat /sys/fs/cgroup/cgroup.subtree_control)" \
  "$(cat /proc/1/cgroup)"
