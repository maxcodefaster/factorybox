import unittest
import mount_ref_events as trace

class FileLifetimeEvents(unittest.TestCase):
    def event(self,ns,operation,*,file=20,generation=100,result=0,mount_id=43):
        return trace.EVENT.pack(ns,file,30,40<<32|41,generation,42,mount_id,operation,result,b'owned\0')

    def test_file_pointer_reuse_keeps_separate_generations(self):
        data=b''.join([self.event(1,1),self.event(2,5,result=-16),self.event(3,3),
                       self.event(4,1,generation=200),self.event(5,5,result=-16),
                       self.event(6,3,generation=200),self.event(7,5,result=0)])
        summary=trace.summarize(trace.decode(data))
        self.assertFalse(summary['complete_mount_reference_inventory'])
        first,second,last=summary['unmounts']
        self.assertEqual(first['observed_file_opens_without_final_release'][0]['generation'],100)
        self.assertEqual(first['observed_file_opens_without_final_release'][0]['observed_final_release_ns'],3)
        self.assertEqual(second['observed_file_opens_without_final_release'][0]['generation'],200)
        self.assertEqual(last['observed_file_opens_without_final_release'],[])

    def test_zero_legacy_mount_id_is_preserved_as_an_observation(self):
        summary=trace.summarize(trace.decode(self.event(1,1,mount_id=0)+self.event(2,3,mount_id=0)))
        self.assertEqual(summary['zero_legacy_mount_id_events'],2)
        self.assertFalse(summary['complete_mount_reference_inventory'])

    def test_truncated_and_unidentified_records_refuse(self):
        for data in [self.event(1,1)[:-1],self.event(1,1,generation=0),self.event(1,9),self.event(0,1)]:
            with self.assertRaises(ValueError):trace.decode(data)

    def test_interpretation_distinguishes_recorded_open_hooks(self):
        rows=trace.decode(self.event(1,1))
        self.assertIn('omit O_PATH',trace.summarize(rows)['interpretation'])
        successful=trace.summarize(rows,open_hook='path_openat')
        self.assertIn('including O_PATH',successful['interpretation'])
        self.assertFalse(successful['complete_mount_reference_inventory'])
        with self.assertRaises(ValueError):trace.summarize(rows,open_hook='unknown')

if __name__ == '__main__':unittest.main()
